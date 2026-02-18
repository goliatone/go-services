package salesforce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestNew_UsesClientCredentialsAuthAndCapabilities(t *testing.T) {
	providerRaw, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     "https://auth.example/token",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider, ok := providerRaw.(*Provider)
	if !ok {
		t.Fatalf("expected *Provider")
	}
	if provider.AuthKind() != core.AuthKindOAuth2ClientCredential {
		t.Fatalf("expected auth kind %q, got %q", core.AuthKindOAuth2ClientCredential, provider.AuthKind())
	}
	if len(provider.Capabilities()) == 0 {
		t.Fatalf("expected capabilities")
	}
	if provider.AuthStrategy() == nil {
		t.Fatalf("expected auth strategy")
	}
}

func TestProvider_CompleteAuthNormalizesGrants(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			http.Error(w, "unsupported grant type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token_1",
			"token_type":   "bearer",
			"expires_in":   3600,
			"scope":        "api bulk_api",
		})
	}))
	defer tokenServer.Close()

	providerRaw, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     tokenServer.URL,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider := providerRaw.(*Provider)
	complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_1"},
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if complete.Credential.AccessToken == "" {
		t.Fatalf("expected access token")
	}
	grants := map[string]bool{}
	for _, grant := range complete.GrantedGrants {
		grants[grant] = true
	}
	if !grants[GrantAPIRead] {
		t.Fatalf("expected %q to be granted", GrantAPIRead)
	}
	if !grants[GrantBulkExport] {
		t.Fatalf("expected %q to be granted", GrantBulkExport)
	}
}

func TestProvider_ResolveCapabilityOperation_UsesDegradeForOptionalGrantMiss(t *testing.T) {
	providerRaw, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     "https://auth.example/token",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider := providerRaw.(*Provider)

	degraded, err := provider.ResolveCapabilityOperation(context.Background(), core.CapabilityOperationResolveRequest{
		ProviderID: "salesforce",
		Scope:      core.ScopeRef{Type: "org", ID: "org_1"},
		Connection: core.Connection{ID: "conn_1"},
		Capability: "crm.accounts.bulk_export",
		Decision: core.CapabilityResult{
			Allowed: true,
			Mode:    core.CapabilityDeniedBehaviorDegrade,
		},
	})
	if err != nil {
		t.Fatalf("resolve degraded operation: %v", err)
	}
	if degraded.TransportKind != "rest" {
		t.Fatalf("expected degraded transport kind rest, got %q", degraded.TransportKind)
	}

	full, err := provider.ResolveCapabilityOperation(context.Background(), core.CapabilityOperationResolveRequest{
		ProviderID: "salesforce",
		Scope:      core.ScopeRef{Type: "org", ID: "org_1"},
		Connection: core.Connection{ID: "conn_1"},
		Capability: "crm.accounts.bulk_export",
		Decision: core.CapabilityResult{
			Allowed: true,
			Mode:    core.CapabilityDeniedBehaviorBlock,
		},
	})
	if err != nil {
		t.Fatalf("resolve full operation: %v", err)
	}
	if full.TransportKind != "bulk" {
		t.Fatalf("expected full transport kind bulk, got %q", full.TransportKind)
	}
}
