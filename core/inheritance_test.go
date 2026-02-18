package core

import (
	"context"
	"testing"
)

func TestInvokeCapability_StrictIsolationByDefault(t *testing.T) {
	ctx := context.Background()
	store := newMemoryConnectionStore()
	_, err := store.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u1"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{
		id: "github",
		capabilities: []CapabilityDescriptor{{
			Name:           "repo.read",
			DeniedBehavior: CapabilityDeniedBehaviorBlock,
		}},
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(store),
		WithInheritancePolicy(staticInheritancePolicy{resolution: ConnectionResolution{Outcome: ConnectionResolutionInherited}}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.InvokeCapability(ctx, InvokeCapabilityRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "org", ID: "o1"},
		Capability: "repo.read",
	})
	if err != nil {
		t.Fatalf("invoke capability: %v", err)
	}
	if result.Allowed {
		t.Fatalf("expected blocked result under strict isolation")
	}
}

func TestInvokeCapability_UsesInheritancePolicyWhenProviderEnabled(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{
		id: "github",
		capabilities: []CapabilityDescriptor{{
			Name:           "repo.read",
			DeniedBehavior: CapabilityDeniedBehaviorBlock,
		}},
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	inheritedConnection := Connection{ID: "conn_inherited", ProviderID: "github", Status: ConnectionStatusActive}
	svc, err := NewService(Config{
		ServiceName: "services",
		Inheritance: InheritanceConfig{EnabledProviders: []string{"github"}},
	},
		WithRegistry(registry),
		WithInheritancePolicy(staticInheritancePolicy{resolution: ConnectionResolution{
			Outcome:    ConnectionResolutionInherited,
			Connection: inheritedConnection,
		}}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.InvokeCapability(ctx, InvokeCapabilityRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "org", ID: "o1"},
		Capability: "repo.read",
	})
	if err != nil {
		t.Fatalf("invoke capability: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected inherited invocation to be allowed")
	}
	if result.Connection.ID != "conn_inherited" {
		t.Fatalf("expected inherited connection id, got %q", result.Connection.ID)
	}
}

func TestInvokeCapability_StrictIsolationFailsClosedWhenScopeIsAmbiguous(t *testing.T) {
	ctx := context.Background()
	store := newMemoryConnectionStore()
	_, err := store.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u2"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create first connection: %v", err)
	}
	_, err = store.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u2"},
		ExternalAccountID: "acct_2",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create second connection: %v", err)
	}

	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{
		id: "github",
		capabilities: []CapabilityDescriptor{{
			Name:           "repo.read",
			DeniedBehavior: CapabilityDeniedBehaviorBlock,
		}},
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(store),
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
	if result.Allowed {
		t.Fatalf("expected blocked result when multiple active scoped connections exist")
	}
	if result.Reason == "" {
		t.Fatalf("expected ambiguity reason")
	}
}

func TestInvokeCapability_UsesExplicitConnectionIDWhenScopeIsAmbiguous(t *testing.T) {
	ctx := context.Background()
	store := newMemoryConnectionStore()
	first, err := store.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u3"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create first connection: %v", err)
	}
	second, err := store.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u3"},
		ExternalAccountID: "acct_2",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create second connection: %v", err)
	}

	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{
		id: "github",
		capabilities: []CapabilityDescriptor{{
			Name:           "repo.read",
			DeniedBehavior: CapabilityDeniedBehaviorBlock,
		}},
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(store),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.InvokeCapability(ctx, InvokeCapabilityRequest{
		ProviderID:   "github",
		ConnectionID: second.ID,
		Capability:   "repo.read",
	})
	if err != nil {
		t.Fatalf("invoke capability: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected allowed result with explicit connection id")
	}
	if result.Connection.ID != second.ID {
		t.Fatalf("expected selected connection %q, got %q", second.ID, result.Connection.ID)
	}
	if result.Connection.ID == first.ID {
		t.Fatalf("expected explicit connection id selection, got fallback connection")
	}
}
