package pinterest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestNew_UsesOAuthDefaults(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret"})
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
		Scope: core.ScopeRef{Type: "user", ID: "usr_1"},
		State: "state_1",
	})
	if err != nil {
		t.Fatalf("begin auth: %v", err)
	}
	parsed, err := url.Parse(begin.URL)
	if err != nil {
		t.Fatalf("parse begin auth url: %v", err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != AuthURL {
		t.Fatalf("expected begin auth endpoint %q, got %q", AuthURL, parsed.Scheme+"://"+parsed.Host+parsed.Path)
	}

	scopeSet := parseScopeSet(parsed.Query().Get("scope"))
	for _, scope := range []string{ScopeUserAccountsRead, ScopeBoardsRead, ScopePinsRead} {
		if !scopeSet[scope] {
			t.Fatalf("expected scope %q in begin auth query", scope)
		}
	}
}

func TestProvider_BaselineCapabilitiesUseCanonicalRequiredGrants(t *testing.T) {
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	descriptors := provider.Capabilities()
	expected := map[string]string{
		"account.read": GrantUserAccountsRead,
		"boards.read":  GrantBoardsRead,
		"pins.read":    GrantPinsRead,
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
		if got := strings.TrimSpace(r.Form.Get("client_id")); got != "client" {
			http.Error(w, "missing client_id in form body", http.StatusBadRequest)
			return
		}
		if !hasBasicAuth(r.Header.Get("Authorization"), "client", "secret") {
			http.Error(w, "authorization header must contain client credentials", http.StatusBadRequest)
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
				"scope":         "user_accounts:read boards:read pins:read",
			})
		case "refresh_token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access_token_2",
				"refresh_token": "refresh_token_2",
				"token_type":    "bearer",
				"expires_in":    3600,
				"scope":         "user_accounts:read,pins:read",
			})
		default:
			http.Error(w, "unsupported grant type", http.StatusBadRequest)
		}
	}))
	defer tokenServer.Close()

	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      AuthURL,
		TokenURL:     tokenServer.URL,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
		Scope:    core.ScopeRef{Type: "user", ID: "usr_1"},
		Code:     "code_1",
		State:    "state_1",
		Metadata: map[string]any{"external_account_id": "pin_user_1"},
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if complete.ExternalAccountID != "pin_user_1" {
		t.Fatalf("expected external account id pin_user_1, got %q", complete.ExternalAccountID)
	}
	if complete.Credential.AccessToken != "access_token_1" {
		t.Fatalf("expected access token from callback")
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
	provider, err := New(Config{ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	aware, ok := provider.(core.GrantAwareProvider)
	if !ok {
		t.Fatalf("expected provider to implement GrantAwareProvider")
	}
	grants, err := aware.NormalizeGrantedPermissions(context.Background(), []string{
		"pins:read",
		"pinterest:user_accounts:read",
		" PINTEREST:boards:read ",
		"unsupported",
	})
	if err != nil {
		t.Fatalf("normalize granted permissions: %v", err)
	}

	expected := []string{GrantBoardsRead, GrantPinsRead, GrantUserAccountsRead}
	if len(grants) != len(expected) {
		t.Fatalf("expected %d grants, got %d (%v)", len(expected), len(grants), grants)
	}
	for idx := range expected {
		if grants[idx] != expected[idx] {
			t.Fatalf("expected grant %q at index %d, got %q", expected[idx], idx, grants[idx])
		}
	}
}

func parseScopeSet(raw string) map[string]bool {
	set := map[string]bool{}
	for _, scope := range strings.Fields(raw) {
		set[strings.TrimSpace(scope)] = true
	}
	return set
}

func hasBasicAuth(header string, expectedUser string, expectedPass string) bool {
	if !strings.HasPrefix(strings.TrimSpace(header), "Basic ") {
		return false
	}
	encoded := strings.TrimSpace(strings.TrimPrefix(header, "Basic "))
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	return string(decoded) == expectedUser+":"+expectedPass
}
