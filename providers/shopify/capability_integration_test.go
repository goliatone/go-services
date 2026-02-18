package shopify

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestCapabilityPermissionEnforcement(t *testing.T) {
	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		ShopDomain:   "merchant",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	registry := core.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connection := core.Connection{
		ID:         "conn_shopify_1",
		ProviderID: ProviderID,
		ScopeType:  "org",
		ScopeID:    "org_1",
		Status:     core.ConnectionStatusActive,
	}
	connectionStore := &connectionStoreStub{connection: connection}
	grantStore := &grantStoreStub{snapshot: core.GrantSnapshot{
		ConnectionID: connection.ID,
		Version:      1,
		Requested:    []string{GrantReadOrders, GrantReadProducts},
		Granted:      []string{GrantReadProducts},
		CapturedAt:   time.Now().UTC(),
	}}

	svc, err := core.NewService(
		core.Config{},
		core.WithRegistry(registry),
		core.WithConnectionStore(connectionStore),
		core.WithGrantStore(grantStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	blocked, err := svc.InvokeCapability(context.Background(), core.InvokeCapabilityRequest{
		ProviderID:   ProviderID,
		ConnectionID: connection.ID,
		Capability:   "orders.read",
	})
	if err != nil {
		t.Fatalf("invoke blocked capability: %v", err)
	}
	if blocked.Allowed {
		t.Fatalf("expected orders.read to be blocked when read_orders grant is missing")
	}
	if blocked.Mode != core.CapabilityDeniedBehaviorBlock {
		t.Fatalf("expected block mode, got %q", blocked.Mode)
	}

	grantStore.snapshot.Granted = []string{GrantReadProducts, GrantReadOrders}
	allowed, err := svc.InvokeCapability(context.Background(), core.InvokeCapabilityRequest{
		ProviderID:   ProviderID,
		ConnectionID: connection.ID,
		Capability:   "orders.read",
	})
	if err != nil {
		t.Fatalf("invoke allowed capability: %v", err)
	}
	if !allowed.Allowed {
		t.Fatalf("expected orders.read to be allowed when required grant exists")
	}
}

type connectionStoreStub struct {
	connection core.Connection
}

func (s *connectionStoreStub) Create(_ context.Context, _ core.CreateConnectionInput) (core.Connection, error) {
	return s.connection, nil
}

func (s *connectionStoreStub) Get(_ context.Context, id string) (core.Connection, error) {
	if id != s.connection.ID {
		return core.Connection{}, fmt.Errorf("connection %q not found", id)
	}
	return s.connection, nil
}

func (s *connectionStoreStub) FindByScope(
	_ context.Context,
	providerID string,
	scope core.ScopeRef,
) ([]core.Connection, error) {
	if providerID == s.connection.ProviderID &&
		scope.Type == s.connection.ScopeType &&
		scope.ID == s.connection.ScopeID {
		return []core.Connection{s.connection}, nil
	}
	return []core.Connection{}, nil
}

func (s *connectionStoreStub) FindByScopeAndExternalAccount(
	_ context.Context,
	providerID string,
	scope core.ScopeRef,
	externalAccountID string,
) (core.Connection, bool, error) {
	if providerID == s.connection.ProviderID &&
		scope.Type == s.connection.ScopeType &&
		scope.ID == s.connection.ScopeID &&
		externalAccountID == s.connection.ExternalAccountID {
		return s.connection, true, nil
	}
	return core.Connection{}, false, nil
}

func (s *connectionStoreStub) UpdateStatus(
	_ context.Context,
	id string,
	status string,
	reason string,
) error {
	if id != s.connection.ID {
		return fmt.Errorf("connection %q not found", id)
	}
	_ = reason
	s.connection.Status = core.ConnectionStatus(status)
	return nil
}

type grantStoreStub struct {
	snapshot core.GrantSnapshot
}

func (s *grantStoreStub) SaveSnapshot(_ context.Context, in core.SaveGrantSnapshotInput) error {
	s.snapshot = core.GrantSnapshot{
		ConnectionID: in.ConnectionID,
		Version:      in.Version,
		Requested:    append([]string(nil), in.Requested...),
		Granted:      append([]string(nil), in.Granted...),
		CapturedAt:   in.CapturedAt,
		Metadata:     copyMetadata(in.Metadata),
	}
	return nil
}

func (s *grantStoreStub) GetLatestSnapshot(_ context.Context, connectionID string) (core.GrantSnapshot, bool, error) {
	if s.snapshot.ConnectionID != connectionID {
		return core.GrantSnapshot{}, false, nil
	}
	return s.snapshot, true, nil
}

func (s *grantStoreStub) AppendEvent(context.Context, core.AppendGrantEventInput) error {
	return nil
}

func copyMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

var _ core.ConnectionStore = (*connectionStoreStub)(nil)
var _ core.GrantStore = (*grantStoreStub)(nil)
