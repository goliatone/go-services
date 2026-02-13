package core

import (
	"context"
	"testing"
)

func TestInvokeCapability_BlocksWhenRequiredGrantMissing(t *testing.T) {
	ctx := context.Background()

	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{
		id: "github",
		capabilities: []CapabilityDescriptor{{
			Name:           "repo.write",
			RequiredGrants: []string{"repo:write"},
			DeniedBehavior: CapabilityDeniedBehaviorBlock,
		}},
	}); err != nil {
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
		Requested:    []string{"repo:write"},
		Granted:      []string{"repo:read"},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithGrantStore(grantStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.InvokeCapability(ctx, InvokeCapabilityRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "user", ID: "u1"},
		Capability: "repo.write",
	})
	if err != nil {
		t.Fatalf("invoke capability: %v", err)
	}
	if result.Allowed {
		t.Fatalf("expected blocked capability due to missing required grant")
	}
	if result.Mode != CapabilityDeniedBehaviorBlock {
		t.Fatalf("expected block mode, got %q", result.Mode)
	}
}

func TestInvokeCapability_DegradesWhenOptionalGrantMissing(t *testing.T) {
	ctx := context.Background()

	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{
		id: "github",
		capabilities: []CapabilityDescriptor{{
			Name:           "repo.read",
			RequiredGrants: []string{"repo:read"},
			OptionalGrants: []string{"repo:write"},
			DeniedBehavior: CapabilityDeniedBehaviorDegrade,
		}},
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u2"},
		ExternalAccountID: "acct_2",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	grantStore := newMemoryGrantStore()
	if err := grantStore.SaveSnapshot(ctx, SaveGrantSnapshotInput{
		ConnectionID: connection.ID,
		Version:      1,
		Requested:    []string{"repo:read", "repo:write"},
		Granted:      []string{"repo:read"},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithGrantStore(grantStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.InvokeCapability(ctx, InvokeCapabilityRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "user", ID: "u2"},
		Capability: "repo.read",
	})
	if err != nil {
		t.Fatalf("invoke capability: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected capability to be allowed in degrade mode")
	}
	if result.Mode != CapabilityDeniedBehaviorDegrade {
		t.Fatalf("expected degrade mode, got %q", result.Mode)
	}
}
