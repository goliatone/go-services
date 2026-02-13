package core

import (
	"context"
	"testing"
)

func TestInvokeCapability_StrictIsolationByDefault(t *testing.T) {
	ctx := context.Background()
	store := newMemoryConnectionStore()
	_, err := store.Create(ctx, CreateConnectionInput{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "user", ID: "u1"},
		Status:     ConnectionStatusActive,
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
