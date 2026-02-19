package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestInvokeCapabilityOperation_ExecutesResolvedOperation(t *testing.T) {
	ctx := context.Background()
	provider := &capabilityResolverProvider{
		id: "advanced_runtime",
		capabilities: []CapabilityDescriptor{{
			Name:           "reports.read",
			RequiredGrants: []string{"reports.read"},
			DeniedBehavior: CapabilityDeniedBehaviorBlock,
		}},
		resolve: func(_ context.Context, req CapabilityOperationResolveRequest) (ProviderOperationRequest, error) {
			if req.Decision.Mode != CapabilityDeniedBehaviorBlock {
				t.Fatalf("expected decision mode block for allowed path")
			}
			return ProviderOperationRequest{
				Operation: "reports.fetch",
				Adapter: &capabilityRuntimeAdapter{
					kind: "bulk",
					res:  TransportResponse{StatusCode: 200, Body: []byte(`{"ok":true}`)},
				},
				Credential: &ActiveCredential{
					TokenType:   "bearer",
					AccessToken: "token_1",
				},
				TransportKind: "bulk",
				TransportRequest: TransportRequest{
					Method: "POST",
					URL:    "https://example.test/reports",
				},
				Retry: ProviderOperationRetryPolicy{MaxAttempts: 1},
			}, nil
		},
	}

	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        provider.id,
		Scope:             ScopeRef{Type: "org", ID: "org_1"},
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
		Requested:    []string{"reports.read"},
		Granted:      []string{"reports.read"},
		CapturedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save grant snapshot: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithGrantStore(grantStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.InvokeCapabilityOperation(ctx, InvokeCapabilityOperationRequest{
		ProviderID:   provider.id,
		ConnectionID: connection.ID,
		Capability:   "reports.read",
		Scope:        ScopeRef{Type: "org", ID: "org_1"},
		Payload:      map[string]any{"limit": 25},
	})
	if err != nil {
		t.Fatalf("invoke capability operation: %v", err)
	}
	if !result.Capability.Allowed {
		t.Fatalf("expected capability decision to be allowed")
	}
	if !result.Executed {
		t.Fatalf("expected operation to execute")
	}
	if result.Operation.Response.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", result.Operation.Response.StatusCode)
	}
	if result.Operation.Operation != "reports.fetch" {
		t.Fatalf("expected operation reports.fetch, got %q", result.Operation.Operation)
	}
}

func TestInvokeCapabilityOperation_ReturnsBlockedDecisionWithoutExecution(t *testing.T) {
	ctx := context.Background()
	provider := &capabilityResolverProvider{
		id: "advanced_runtime_blocked",
		capabilities: []CapabilityDescriptor{{
			Name:           "reports.read",
			RequiredGrants: []string{"reports.read"},
			DeniedBehavior: CapabilityDeniedBehaviorBlock,
		}},
		resolve: func(context.Context, CapabilityOperationResolveRequest) (ProviderOperationRequest, error) {
			return ProviderOperationRequest{}, fmt.Errorf("resolver should not execute for blocked decision")
		},
	}

	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        provider.id,
		Scope:             ScopeRef{Type: "org", ID: "org_1"},
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
		Requested:    []string{"reports.read"},
		Granted:      []string{},
		CapturedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save grant snapshot: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithGrantStore(grantStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.InvokeCapabilityOperation(ctx, InvokeCapabilityOperationRequest{
		ProviderID:   provider.id,
		ConnectionID: connection.ID,
		Capability:   "reports.read",
		Scope:        ScopeRef{Type: "org", ID: "org_1"},
	})
	if err != nil {
		t.Fatalf("invoke blocked capability operation: %v", err)
	}
	if result.Capability.Allowed {
		t.Fatalf("expected blocked decision")
	}
	if result.Executed {
		t.Fatalf("expected operation not to execute for blocked decision")
	}
}

func TestInvokeCapabilityOperation_RequiresCapabilityOperationResolver(t *testing.T) {
	ctx := context.Background()
	provider := testProvider{id: "no_runtime_provider", capabilities: []CapabilityDescriptor{{Name: "reports.read"}}}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        provider.id,
		Scope:             ScopeRef{Type: "org", ID: "org_1"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.InvokeCapabilityOperation(ctx, InvokeCapabilityOperationRequest{
		ProviderID:   provider.id,
		ConnectionID: connection.ID,
		Capability:   "reports.read",
		Scope:        ScopeRef{Type: "org", ID: "org_1"},
	})
	if err == nil {
		t.Fatalf("expected resolver requirement error")
	}
	if !strings.Contains(err.Error(), "does not support capability operation runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type capabilityResolverProvider struct {
	id           string
	capabilities []CapabilityDescriptor
	resolve      func(ctx context.Context, req CapabilityOperationResolveRequest) (ProviderOperationRequest, error)
}

func (p *capabilityResolverProvider) ID() string { return p.id }

func (p *capabilityResolverProvider) AuthKind() AuthKind { return AuthKindOAuth2AuthCode }

func (p *capabilityResolverProvider) SupportedScopeTypes() []string { return []string{"org"} }

func (p *capabilityResolverProvider) Capabilities() []CapabilityDescriptor {
	return append([]CapabilityDescriptor(nil), p.capabilities...)
}

func (p *capabilityResolverProvider) BeginAuth(context.Context, BeginAuthRequest) (BeginAuthResponse, error) {
	return BeginAuthResponse{URL: "https://example.test/auth", State: "state"}, nil
}

func (p *capabilityResolverProvider) CompleteAuth(context.Context, CompleteAuthRequest) (CompleteAuthResponse, error) {
	return CompleteAuthResponse{ExternalAccountID: "acct_1", Credential: ActiveCredential{AccessToken: "token"}}, nil
}

func (p *capabilityResolverProvider) Refresh(context.Context, ActiveCredential) (RefreshResult, error) {
	return RefreshResult{Credential: ActiveCredential{AccessToken: "token"}}, nil
}

func (p *capabilityResolverProvider) ResolveCapabilityOperation(
	ctx context.Context,
	req CapabilityOperationResolveRequest,
) (ProviderOperationRequest, error) {
	if p.resolve == nil {
		return ProviderOperationRequest{}, fmt.Errorf("resolver not configured")
	}
	return p.resolve(ctx, req)
}

type capabilityRuntimeAdapter struct {
	kind string
	res  TransportResponse
	err  error
}

func (a *capabilityRuntimeAdapter) Kind() string {
	if a == nil {
		return ""
	}
	if strings.TrimSpace(a.kind) == "" {
		return "rest"
	}
	return a.kind
}

func (a *capabilityRuntimeAdapter) Do(context.Context, TransportRequest) (TransportResponse, error) {
	if a == nil {
		return TransportResponse{}, fmt.Errorf("adapter is nil")
	}
	return a.res, a.err
}

var _ Provider = (*capabilityResolverProvider)(nil)
var _ CapabilityOperationResolver = (*capabilityResolverProvider)(nil)
var _ TransportAdapter = (*capabilityRuntimeAdapter)(nil)
