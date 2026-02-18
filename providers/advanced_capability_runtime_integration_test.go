package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/salesforce"
	"github.com/goliatone/go-services/providers/workday"
	"github.com/goliatone/go-services/transport"
)

func TestAdvancedCapabilityRuntime_SalesforceGrantEnforcementAndTransportInterop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	providerRaw, err := salesforce.New(salesforce.Config{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL + "/oauth/token",
		InstanceURL:  server.URL,
	})
	if err != nil {
		t.Fatalf("new salesforce provider: %v", err)
	}
	provider := providerRaw

	registry := core.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connection := core.Connection{
		ID:                "conn_salesforce_1",
		ProviderID:        provider.ID(),
		ScopeType:         "org",
		ScopeID:           "org_1",
		ExternalAccountID: "acct_salesforce_1",
		Status:            core.ConnectionStatusActive,
	}
	connectionStore := &providerConnectionStoreStub{connection: connection}
	grantStore := &providerGrantStoreStub{snapshot: core.GrantSnapshot{
		ConnectionID: connection.ID,
		Version:      1,
		Requested:    []string{salesforce.GrantAPIRead, salesforce.GrantBulkExport},
		Granted:      []string{},
		CapturedAt:   time.Now().UTC(),
	}}

	svc, err := core.NewService(
		core.Config{},
		core.WithRegistry(registry),
		core.WithConnectionStore(connectionStore),
		core.WithGrantStore(grantStore),
		core.WithTransportResolver(transport.NewDefaultRegistry()),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	blocked, err := svc.InvokeCapabilityOperation(context.Background(), core.InvokeCapabilityOperationRequest{
		ProviderID:   provider.ID(),
		ConnectionID: connection.ID,
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		Capability:   "crm.accounts.read",
	})
	if err != nil {
		t.Fatalf("invoke blocked capability operation: %v", err)
	}
	if blocked.Executed {
		t.Fatalf("expected blocked capability not to execute operation")
	}

	grantStore.snapshot.Granted = []string{salesforce.GrantAPIRead}
	degraded, err := svc.InvokeCapabilityOperation(context.Background(), core.InvokeCapabilityOperationRequest{
		ProviderID:   provider.ID(),
		ConnectionID: connection.ID,
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		Capability:   "crm.accounts.bulk_export",
	})
	if err != nil {
		t.Fatalf("invoke degraded capability operation: %v", err)
	}
	if !degraded.Executed {
		t.Fatalf("expected degraded capability to execute")
	}
	if degraded.Operation.TransportKind != "rest" {
		t.Fatalf("expected degraded transport kind rest, got %q", degraded.Operation.TransportKind)
	}

	grantStore.snapshot.Granted = []string{salesforce.GrantAPIRead, salesforce.GrantBulkExport}
	full, err := svc.InvokeCapabilityOperation(context.Background(), core.InvokeCapabilityOperationRequest{
		ProviderID:   provider.ID(),
		ConnectionID: connection.ID,
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		Capability:   "crm.accounts.bulk_export",
	})
	if err != nil {
		t.Fatalf("invoke full capability operation: %v", err)
	}
	if !full.Executed {
		t.Fatalf("expected full capability to execute")
	}
	if full.Operation.TransportKind != "bulk" {
		t.Fatalf("expected full transport kind bulk, got %q", full.Operation.TransportKind)
	}
	if full.Operation.Response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", full.Operation.Response.StatusCode)
	}
}

func TestAdvancedCapabilityRuntime_WorkdayGrantEnforcementAndTransportInterop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	providerRaw, err := workday.New(workday.Config{
		Issuer:     "svc-account@example.iam.gserviceaccount.com",
		Audience:   server.URL + "/oauth/token",
		SigningKey: "secret-signing-key",
		TenantURL:  server.URL,
	})
	if err != nil {
		t.Fatalf("new workday provider: %v", err)
	}
	provider := providerRaw

	registry := core.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connection := core.Connection{
		ID:                "conn_workday_1",
		ProviderID:        provider.ID(),
		ScopeType:         "org",
		ScopeID:           "org_1",
		ExternalAccountID: "acct_workday_1",
		Status:            core.ConnectionStatusActive,
	}
	connectionStore := &providerConnectionStoreStub{connection: connection}
	grantStore := &providerGrantStoreStub{snapshot: core.GrantSnapshot{
		ConnectionID: connection.ID,
		Version:      1,
		Requested:    []string{workday.GrantHRRead, workday.GrantReportExport},
		Granted:      []string{},
		CapturedAt:   time.Now().UTC(),
	}}

	svc, err := core.NewService(
		core.Config{},
		core.WithRegistry(registry),
		core.WithConnectionStore(connectionStore),
		core.WithGrantStore(grantStore),
		core.WithTransportResolver(transport.NewDefaultRegistry()),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	blocked, err := svc.InvokeCapabilityOperation(context.Background(), core.InvokeCapabilityOperationRequest{
		ProviderID:   provider.ID(),
		ConnectionID: connection.ID,
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		Capability:   "hr.employees.read",
	})
	if err != nil {
		t.Fatalf("invoke blocked capability operation: %v", err)
	}
	if blocked.Executed {
		t.Fatalf("expected blocked capability not to execute operation")
	}

	grantStore.snapshot.Granted = []string{workday.GrantHRRead}
	degraded, err := svc.InvokeCapabilityOperation(context.Background(), core.InvokeCapabilityOperationRequest{
		ProviderID:   provider.ID(),
		ConnectionID: connection.ID,
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		Capability:   "hr.reports.export",
	})
	if err != nil {
		t.Fatalf("invoke degraded capability operation: %v", err)
	}
	if !degraded.Executed {
		t.Fatalf("expected degraded capability to execute")
	}
	if degraded.Operation.TransportKind != "stream" {
		t.Fatalf("expected degraded transport kind stream, got %q", degraded.Operation.TransportKind)
	}

	grantStore.snapshot.Granted = []string{workday.GrantHRRead, workday.GrantReportExport}
	full, err := svc.InvokeCapabilityOperation(context.Background(), core.InvokeCapabilityOperationRequest{
		ProviderID:   provider.ID(),
		ConnectionID: connection.ID,
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		Capability:   "hr.reports.export",
	})
	if err != nil {
		t.Fatalf("invoke full capability operation: %v", err)
	}
	if !full.Executed {
		t.Fatalf("expected full capability to execute")
	}
	if full.Operation.TransportKind != "file" {
		t.Fatalf("expected full transport kind file, got %q", full.Operation.TransportKind)
	}
	if full.Operation.Response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", full.Operation.Response.StatusCode)
	}
}
