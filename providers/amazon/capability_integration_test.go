package amazon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestCapabilityPermissionEnforcement(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret", SigV4: testSigV4Config()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	registry := core.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connection := core.Connection{
		ID:         "conn_amazon_1",
		ProviderID: ProviderID,
		ScopeType:  "org",
		ScopeID:    "org_1",
		Status:     core.ConnectionStatusActive,
	}
	connectionStore := &amazonConnectionStoreStub{connection: connection}
	grantStore := &amazonGrantStoreStub{snapshot: core.GrantSnapshot{
		ConnectionID: connection.ID,
		Version:      1,
		Requested:    []string{GrantOrdersRead, GrantCatalogRead},
		Granted:      []string{GrantCatalogRead},
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
		t.Fatalf("expected orders.read to be blocked when required grant is missing")
	}
	if blocked.Mode != core.CapabilityDeniedBehaviorBlock {
		t.Fatalf("expected block mode, got %q", blocked.Mode)
	}

	grantStore.snapshot.Granted = []string{GrantCatalogRead, GrantOrdersRead}
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

type amazonConnectionStoreStub struct {
	connection core.Connection
}

func (s *amazonConnectionStoreStub) Create(_ context.Context, _ core.CreateConnectionInput) (core.Connection, error) {
	return s.connection, nil
}

func (s *amazonConnectionStoreStub) Get(_ context.Context, id string) (core.Connection, error) {
	if id != s.connection.ID {
		return core.Connection{}, fmt.Errorf("connection %q not found", id)
	}
	return s.connection, nil
}

func (s *amazonConnectionStoreStub) FindByScope(
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

func (s *amazonConnectionStoreStub) FindByScopeAndExternalAccount(
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

func (s *amazonConnectionStoreStub) UpdateStatus(
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

type amazonGrantStoreStub struct {
	snapshot core.GrantSnapshot
}

func (s *amazonGrantStoreStub) SaveSnapshot(_ context.Context, in core.SaveGrantSnapshotInput) error {
	s.snapshot = core.GrantSnapshot{
		ConnectionID: in.ConnectionID,
		Version:      in.Version,
		Requested:    append([]string(nil), in.Requested...),
		Granted:      append([]string(nil), in.Granted...),
		CapturedAt:   in.CapturedAt,
		Metadata:     copyAmazonMetadata(in.Metadata),
	}
	return nil
}

func (s *amazonGrantStoreStub) GetLatestSnapshot(_ context.Context, connectionID string) (core.GrantSnapshot, bool, error) {
	if s.snapshot.ConnectionID != connectionID {
		return core.GrantSnapshot{}, false, nil
	}
	return s.snapshot, true, nil
}

func (s *amazonGrantStoreStub) AppendEvent(context.Context, core.AppendGrantEventInput) error {
	return nil
}

func copyAmazonMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

var _ core.ConnectionStore = (*amazonConnectionStoreStub)(nil)
var _ core.GrantStore = (*amazonGrantStoreStub)(nil)
