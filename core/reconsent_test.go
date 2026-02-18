package core

import (
	"context"
	"testing"
	"time"
)

func TestStartReconsent_TransitionsConnectionAndReturnsAuthResponse(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u1"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	grantStore := newMemoryGrantStore()
	if err := grantStore.SaveSnapshot(ctx, SaveGrantSnapshotInput{
		ConnectionID: connection.ID,
		Version:      1,
		Requested:    []string{"repo:read"},
		Granted:      []string{"repo:read"},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithGrantStore(grantStore),
		WithOAuthStateStore(NewMemoryOAuthStateStore(time.Minute)),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	resp, err := svc.StartReconsent(ctx, ReconsentRequest{
		ConnectionID: connection.ID,
		RedirectURI:  "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("start reconsent: %v", err)
	}
	if resp.State == "" {
		t.Fatalf("expected callback state")
	}

	updated, err := connectionStore.Get(ctx, connection.ID)
	if err != nil {
		t.Fatalf("get updated connection: %v", err)
	}
	if updated.Status != ConnectionStatusNeedsReconsent {
		t.Fatalf("expected needs_reconsent status, got %q", updated.Status)
	}
}

func TestCompleteReconsent_RecoversExistingConnectionState(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u2"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	if err := connectionStore.UpdateStatus(ctx, connection.ID, string(ConnectionStatusNeedsReconsent), "missing grants"); err != nil {
		t.Fatalf("update connection status: %v", err)
	}

	credentialStore := newMemoryCredentialStore()
	grantStore := newMemoryGrantStore()
	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
		WithSecretProvider(testSecretProvider{}),
		WithGrantStore(grantStore),
		WithOAuthStateStore(NewMemoryOAuthStateStore(time.Minute)),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	beginResp, err := svc.StartReconsent(ctx, ReconsentRequest{
		ConnectionID: connection.ID,
		RedirectURI:  "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("start reconsent: %v", err)
	}

	completed, err := svc.CompleteReconsent(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u2"},
		Code:        "code",
		State:       beginResp.State,
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("complete reconsent: %v", err)
	}
	if completed.Connection.ID != connection.ID {
		t.Fatalf("expected existing connection to be recovered, got %q want %q", completed.Connection.ID, connection.ID)
	}

	updated, err := connectionStore.Get(ctx, connection.ID)
	if err != nil {
		t.Fatalf("get updated connection: %v", err)
	}
	if updated.Status != ConnectionStatusActive {
		t.Fatalf("expected recovered active status, got %q", updated.Status)
	}

	events := grantStore.Events(connection.ID)
	found := false
	for _, event := range events {
		if event.EventType == GrantEventReconsentCompleted {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected reconsent_completed event")
	}
}
