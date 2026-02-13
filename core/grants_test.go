package core

import (
	"context"
	"testing"
	"time"
)

func TestComputeGrantDelta(t *testing.T) {
	delta := ComputeGrantDelta([]string{"repo:read", "repo:write"}, []string{"repo:read"})
	if delta.EventType != GrantEventDowngraded {
		t.Fatalf("expected downgraded event, got %q", delta.EventType)
	}
	if len(delta.Removed) != 1 || delta.Removed[0] != "repo:write" {
		t.Fatalf("expected removed repo:write, got %#v", delta.Removed)
	}
}

func TestCompleteCallback_PersistsGrantSnapshotAndEvent(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	provider := &grantScenarioProvider{
		id: "github",
		completeResponse: CompleteAuthResponse{
			ExternalAccountID: "acct_1",
			Credential: ActiveCredential{
				TokenType:       "bearer",
				RequestedScopes: []string{"repo:read"},
				GrantedScopes:   []string{"repo:read"},
				Refreshable:     true,
			},
			RequestedGrants: []string{"repo:read"},
			GrantedGrants:   []string{"repo:read"},
		},
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	credentialStore := newMemoryCredentialStore()
	grantStore := newMemoryGrantStore()
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
		WithGrantStore(grantStore),
		WithOAuthStateStore(NewMemoryOAuthStateStore(time.Minute)),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connectResp, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:      "github",
		Scope:           ScopeRef{Type: "user", ID: "u1"},
		RedirectURI:     "https://app.example/callback",
		RequestedGrants: []string{"repo:read"},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	completed, err := svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u1"},
		Code:        "code",
		State:       connectResp.State,
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("complete callback: %v", err)
	}

	snapshots := grantStore.Snapshots(completed.Connection.ID)
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Version != 1 {
		t.Fatalf("expected snapshot version 1, got %d", snapshots[0].Version)
	}

	events := grantStore.Events(completed.Connection.ID)
	if len(events) != 1 {
		t.Fatalf("expected one grant event, got %d", len(events))
	}
	if events[0].EventType != GrantEventExpanded {
		t.Fatalf("expected expanded event, got %q", events[0].EventType)
	}
}

func TestRefresh_PersistsGrantDeltaEvent(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	provider := &grantScenarioProvider{
		id: "github",
		completeResponse: CompleteAuthResponse{
			ExternalAccountID: "acct_2",
			Credential: ActiveCredential{
				TokenType:       "bearer",
				RequestedScopes: []string{"repo:read", "repo:write"},
				GrantedScopes:   []string{"repo:read", "repo:write"},
				Refreshable:     true,
			},
			RequestedGrants: []string{"repo:read", "repo:write"},
			GrantedGrants:   []string{"repo:read", "repo:write"},
		},
		refreshResponse: RefreshResult{
			Credential: ActiveCredential{
				TokenType:       "bearer",
				RequestedScopes: []string{"repo:read", "repo:write"},
				GrantedScopes:   []string{"repo:read"},
				Refreshable:     true,
			},
			GrantedGrants: []string{"repo:read"},
		},
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	credentialStore := newMemoryCredentialStore()
	grantStore := newMemoryGrantStore()
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
		WithGrantStore(grantStore),
		WithOAuthStateStore(NewMemoryOAuthStateStore(time.Minute)),
		WithRefreshBackoffScheduler(ExponentialBackoffScheduler{Initial: 0, Max: 0}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connectResp, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:      "github",
		Scope:           ScopeRef{Type: "user", ID: "u2"},
		RedirectURI:     "https://app.example/callback",
		RequestedGrants: []string{"repo:read", "repo:write"},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	completed, err := svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u2"},
		Code:        "code",
		State:       connectResp.State,
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("complete callback: %v", err)
	}

	if _, err := svc.Refresh(ctx, RefreshRequest{
		ProviderID:   "github",
		ConnectionID: completed.Connection.ID,
	}); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	snapshots := grantStore.Snapshots(completed.Connection.ID)
	if len(snapshots) != 2 {
		t.Fatalf("expected two snapshots, got %d", len(snapshots))
	}
	if snapshots[1].Version != 2 {
		t.Fatalf("expected snapshot version 2, got %d", snapshots[1].Version)
	}

	events := grantStore.Events(completed.Connection.ID)
	if len(events) != 2 {
		t.Fatalf("expected two grant events, got %d", len(events))
	}
	last := events[len(events)-1]
	if last.EventType != GrantEventDowngraded {
		t.Fatalf("expected downgraded event, got %q", last.EventType)
	}
}

type grantScenarioProvider struct {
	id               string
	completeResponse CompleteAuthResponse
	refreshResponse  RefreshResult
}

func (p *grantScenarioProvider) ID() string                           { return p.id }
func (p *grantScenarioProvider) AuthKind() string                     { return "oauth2" }
func (p *grantScenarioProvider) SupportedScopeTypes() []string        { return []string{"user", "org"} }
func (p *grantScenarioProvider) Capabilities() []CapabilityDescriptor { return nil }

func (p *grantScenarioProvider) BeginAuth(_ context.Context, req BeginAuthRequest) (BeginAuthResponse, error) {
	return BeginAuthResponse{
		URL:             "https://example.com/auth",
		State:           req.State,
		RequestedGrants: append([]string(nil), req.RequestedGrants...),
	}, nil
}

func (p *grantScenarioProvider) CompleteAuth(_ context.Context, _ CompleteAuthRequest) (CompleteAuthResponse, error) {
	return p.completeResponse, nil
}

func (p *grantScenarioProvider) Refresh(_ context.Context, _ ActiveCredential) (RefreshResult, error) {
	return p.refreshResponse, nil
}
