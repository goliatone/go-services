package shopify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestNew_UsesShopDomainDefaults(t *testing.T) {
	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		ShopDomain:   "merchant-store",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if provider.ID() != ProviderID {
		t.Fatalf("expected provider id %q, got %q", ProviderID, provider.ID())
	}
	if provider.AuthKind() != core.AuthKindOAuth2AuthCode {
		t.Fatalf("expected auth kind %q, got %q", core.AuthKindOAuth2AuthCode, provider.AuthKind())
	}

	begin, err := provider.BeginAuth(context.Background(), core.BeginAuthRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_1"},
		State: "state_1",
	})
	if err != nil {
		t.Fatalf("begin auth: %v", err)
	}
	parsed, err := url.Parse(begin.URL)
	if err != nil {
		t.Fatalf("parse begin auth url: %v", err)
	}
	if parsed.Host != "merchant-store.myshopify.com" {
		t.Fatalf("expected merchant host, got %q", parsed.Host)
	}
	for _, scope := range []string{ScopeReadProducts, ScopeReadInventory, ScopeReadOrders} {
		if !strings.Contains(parsed.Query().Get("scope"), scope) {
			t.Fatalf("expected scope %q in begin auth URL", scope)
		}
	}
}

func TestProvider_BaselineCapabilitiesUseCanonicalRequiredGrants(t *testing.T) {
	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		ShopDomain:   "merchant-store",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	descriptors := provider.Capabilities()
	expected := map[string]string{
		"catalog.read":   GrantReadProducts,
		"inventory.read": GrantReadInventory,
		"orders.read":    GrantReadOrders,
	}
	if len(descriptors) != len(expected) {
		t.Fatalf("expected %d capabilities, got %d", len(expected), len(descriptors))
	}
	for _, descriptor := range descriptors {
		requiredGrant, ok := expected[descriptor.Name]
		if !ok {
			t.Fatalf("unexpected capability descriptor %q", descriptor.Name)
		}
		if len(descriptor.RequiredGrants) != 1 || descriptor.RequiredGrants[0] != requiredGrant {
			t.Fatalf(
				"expected capability %q to require grant %q, got %v",
				descriptor.Name,
				requiredGrant,
				descriptor.RequiredGrants,
			)
		}
	}
}

func TestProvider_CompleteAuthAndRefresh(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if got := strings.TrimSpace(r.Form.Get("client_secret")); got != "secret" {
			http.Error(w, "client_secret must be sent in form body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
			http.Error(w, "authorization header should not be set", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access_token_1",
				"refresh_token": "refresh_token_1",
				"token_type":    "bearer",
				"expires_in":    3600,
				"scope":         "read_products,read_inventory,read_orders",
			})
		case "refresh_token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access_token_2",
				"refresh_token": "refresh_token_2",
				"token_type":    "bearer",
				"expires_in":    3600,
				"scope":         "read_products read_inventory read_orders",
			})
		default:
			http.Error(w, "unsupported grant type", http.StatusBadRequest)
		}
	}))
	defer tokenServer.Close()

	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      "https://merchant.myshopify.com/admin/oauth/authorize",
		TokenURL:     tokenServer.URL,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
		Scope:    core.ScopeRef{Type: "org", ID: "org_1"},
		Code:     "code_1",
		State:    "state_1",
		Metadata: map[string]any{"external_account_id": "shop_1"},
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if complete.ExternalAccountID != "shop_1" {
		t.Fatalf("expected external account id shop_1, got %q", complete.ExternalAccountID)
	}
	if complete.Credential.AccessToken != "access_token_1" {
		t.Fatalf("expected access token from callback")
	}
	if complete.Credential.RefreshToken != "refresh_token_1" {
		t.Fatalf("expected refresh token from callback")
	}

	refresh, err := provider.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	if refresh.Credential.AccessToken != "access_token_2" {
		t.Fatalf("expected refreshed access token, got %q", refresh.Credential.AccessToken)
	}
	if refresh.Credential.RefreshToken != "refresh_token_2" {
		t.Fatalf("expected refresh token rotation")
	}
}

func TestProvider_NormalizeGrantedPermissions(t *testing.T) {
	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		ShopDomain:   "merchant",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	aware, ok := provider.(core.GrantAwareProvider)
	if !ok {
		t.Fatalf("expected provider to implement GrantAwareProvider")
	}
	grants, err := aware.NormalizeGrantedPermissions(context.Background(), []string{
		"read_products",
		"shopify:read_orders",
		" READ_INVENTORY ",
		"",
	})
	if err != nil {
		t.Fatalf("normalize granted permissions: %v", err)
	}

	expected := []string{GrantReadInventory, GrantReadOrders, GrantReadProducts}
	if len(grants) != len(expected) {
		t.Fatalf("expected %d grants, got %d (%v)", len(expected), len(grants), grants)
	}
	for idx := range expected {
		if grants[idx] != expected[idx] {
			t.Fatalf("expected grant %q at index %d, got %q", expected[idx], idx, grants[idx])
		}
	}
}
