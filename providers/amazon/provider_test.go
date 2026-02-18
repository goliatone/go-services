package amazon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestNew_UsesLWADefaults(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret", SigV4: testSigV4Config()})
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
	for _, scope := range []string{ScopeCatalogRead, ScopeInventoryRead, ScopeOrdersRead} {
		if !containsScope(begin.RequestedGrants, scope) {
			t.Fatalf("expected scope %q in requested grants %v", scope, begin.RequestedGrants)
		}
	}
}

func TestNew_RequiresSigV4Credentials(t *testing.T) {
	_, err := New(Config{ClientID: "client", ClientSecret: "secret"})
	if err == nil {
		t.Fatalf("expected missing sigv4 configuration error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "sigv4") {
		t.Fatalf("expected sigv4 config error, got %v", err)
	}
}

func TestProvider_BaselineCapabilitiesUseCanonicalRequiredGrants(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret", SigV4: testSigV4Config()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	descriptors := provider.Capabilities()
	expected := map[string]string{
		"catalog.read":   GrantCatalogRead,
		"inventory.read": GrantInventoryRead,
		"orders.read":    GrantOrdersRead,
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
			t.Fatalf("expected capability %q to require %q, got %v", descriptor.Name, requiredGrant, descriptor.RequiredGrants)
		}
	}
}

func TestProvider_NormalizeGrantedPermissions(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret", SigV4: testSigV4Config()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	aware, ok := provider.(core.GrantAwareProvider)
	if !ok {
		t.Fatalf("expected provider to implement GrantAwareProvider")
	}
	grants, err := aware.NormalizeGrantedPermissions(context.Background(), []string{
		"sellingpartnerapi::orders",
		"amazon:inventory.read",
		" Catalog.Read ",
		"",
	})
	if err != nil {
		t.Fatalf("normalize granted permissions: %v", err)
	}

	expected := []string{GrantCatalogRead, GrantInventoryRead, GrantOrdersRead}
	if len(grants) != len(expected) {
		t.Fatalf("expected %d grants, got %d (%v)", len(expected), len(grants), grants)
	}
	for idx := range expected {
		if grants[idx] != expected[idx] {
			t.Fatalf("expected grant %q at index %d, got %q", expected[idx], idx, grants[idx])
		}
	}
}

func TestProvider_CompleteAuthAndRefreshEmbedSigV4Profile(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimSpace(r.Form.Get("grant_type")) {
		case "authorization_code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "lwa_access_1",
				"refresh_token": "lwa_refresh_1",
				"token_type":    "bearer",
				"expires_in":    3600,
				"scope":         "sellingpartnerapi::orders sellingpartnerapi::catalog",
			})
		case "refresh_token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "lwa_access_2",
				"refresh_token": "lwa_refresh_2",
				"token_type":    "bearer",
				"expires_in":    3600,
				"scope":         "sellingpartnerapi::orders",
			})
		default:
			http.Error(w, "unsupported grant type", http.StatusBadRequest)
		}
	}))
	defer tokenServer.Close()

	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     tokenServer.URL,
		SigV4:        testSigV4Config(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
		Scope:    core.ScopeRef{Type: "org", ID: "org_1"},
		Code:     "code_1",
		State:    "state_1",
		Metadata: map[string]any{"external_account_id": "seller_1"},
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if complete.Credential.AccessToken != "lwa_access_1" {
		t.Fatalf("expected callback token, got %q", complete.Credential.AccessToken)
	}
	if got := complete.Credential.Metadata["auth_kind"]; got != core.AuthKindAWSSigV4 {
		t.Fatalf("expected auth kind metadata %q, got %v", core.AuthKindAWSSigV4, got)
	}
	if got := complete.Credential.Metadata["aws_region"]; got != "us-east-1" {
		t.Fatalf("expected aws_region metadata us-east-1, got %v", got)
	}
	if got := complete.Credential.Metadata["aws_service"]; got != defaultAWSService {
		t.Fatalf("expected aws_service metadata %q, got %v", defaultAWSService, got)
	}

	refresh, err := provider.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	if refresh.Credential.AccessToken != "lwa_access_2" {
		t.Fatalf("expected refreshed access token, got %q", refresh.Credential.AccessToken)
	}
	if got := refresh.Credential.Metadata["auth_kind"]; got != core.AuthKindAWSSigV4 {
		t.Fatalf("expected refresh auth kind metadata %q, got %v", core.AuthKindAWSSigV4, got)
	}
}

func TestProvider_SignerUsesHostAwareSigV4Region(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret", SigV4: testSigV4Config()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	withSigner, ok := provider.(core.ProviderSigner)
	if !ok {
		t.Fatalf("expected ProviderSigner implementation")
	}

	req, err := http.NewRequest(http.MethodGet, "https://sellingpartnerapi-eu.amazon.com/orders/v0/orders", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	cred := core.ActiveCredential{AccessToken: "lwa_token"}
	if err := withSigner.Signer().Sign(context.Background(), req, cred); err != nil {
		t.Fatalf("sign request: %v", err)
	}
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("expected sigv4 authorization, got %q", auth)
	}
	if !strings.Contains(auth, "/eu-west-1/") {
		t.Fatalf("expected eu-west-1 credential scope for EU host, got %q", auth)
	}
	if got := req.Header.Get("X-Amz-Access-Token"); got != "lwa_token" {
		t.Fatalf("expected x-amz-access-token header from oauth token, got %q", got)
	}
}

func testSigV4Config() SigV4Config {
	return SigV4Config{
		AccessKeyID:     "AKIA_TEST",
		SecretAccessKey: "secret_key",
		Region:          "us-east-1",
		Service:         defaultAWSService,
	}
}

func containsScope(scopes []string, target string) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}
