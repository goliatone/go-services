package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/identity"
)

func TestOAuth2Provider_BeginCompleteRefresh(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "code_123" {
				http.Error(w, "invalid code", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access_123",
				"refresh_token": "refresh_123",
				"token_type":    "Bearer",
				"expires_in":    1800,
				"scope":         "repo read:user",
			})
		case "refresh_token":
			if r.Form.Get("refresh_token") != "refresh_123" {
				http.Error(w, "invalid refresh token", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access_456",
				"refresh_token": "refresh_456",
				"token_type":    "bearer",
				"expires_in":    1200,
				"scope":         "repo",
			})
		default:
			http.Error(w, "unsupported grant_type", http.StatusBadRequest)
		}
	}))
	defer tokenServer.Close()

	provider, err := NewOAuth2Provider(OAuth2Config{
		ID:            "github",
		AuthURL:       "https://github.com/login/oauth/authorize",
		TokenURL:      tokenServer.URL,
		ClientID:      "client-123",
		ClientSecret:  "secret-456",
		DefaultScopes: []string{"repo", "read:user"},
		TokenTTL:      30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	begin, err := provider.BeginAuth(context.Background(), core.BeginAuthRequest{
		Scope:       core.ScopeRef{Type: "user", ID: "usr_1"},
		RedirectURI: "https://app.example/callback",
		State:       "state_1",
	})
	if err != nil {
		t.Fatalf("begin auth: %v", err)
	}
	if begin.State != "state_1" {
		t.Fatalf("expected state_1, got %q", begin.State)
	}

	parsed, err := url.Parse(begin.URL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_id") != "client-123" {
		t.Fatalf("expected client_id query value")
	}
	if query.Get("state") != "state_1" {
		t.Fatalf("expected state query value")
	}
	if !strings.Contains(query.Get("scope"), "repo") {
		t.Fatalf("expected scope query to include repo")
	}

	complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
		Scope:    core.ScopeRef{Type: "user", ID: "usr_1"},
		Code:     "code_123",
		State:    begin.State,
		Metadata: map[string]any{"external_account_id": "acct_usr_1"},
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if complete.ExternalAccountID == "" {
		t.Fatalf("expected external account id")
	}
	if complete.ExternalAccountID != "acct_usr_1" {
		t.Fatalf("expected metadata external account id, got %q", complete.ExternalAccountID)
	}
	if complete.Credential.TokenType != "bearer" {
		t.Fatalf("expected bearer token type")
	}
	if complete.Credential.AccessToken != "access_123" {
		t.Fatalf("expected access token from token endpoint")
	}
	if complete.Credential.RefreshToken != "refresh_123" {
		t.Fatalf("expected refresh token from token endpoint")
	}
	if !complete.Credential.Refreshable {
		t.Fatalf("expected refreshable credential")
	}
	if complete.Credential.ExpiresAt == nil {
		t.Fatalf("expected expires at")
	}

	refreshed, err := provider.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatalf("refresh credential: %v", err)
	}
	if refreshed.Credential.AccessToken != "access_456" {
		t.Fatalf("expected refreshed access token from token endpoint")
	}
	if refreshed.Credential.RefreshToken != "refresh_456" {
		t.Fatalf("expected rotated refresh token from token endpoint")
	}
	if refreshed.Credential.ExpiresAt == nil {
		t.Fatalf("expected refreshed expires at")
	}
	if len(refreshed.GrantedGrants) == 0 {
		t.Fatalf("expected granted grants")
	}
}

func TestNewOAuth2Provider_RequiresIDAuthURLTokenURLAndClientID(t *testing.T) {
	_, err := NewOAuth2Provider(OAuth2Config{})
	if err == nil {
		t.Fatalf("expected validation error")
	}

	_, err = NewOAuth2Provider(OAuth2Config{
		ID:       "github",
		AuthURL:  "https://example.com/auth",
		TokenURL: "https://example.com/token",
	})
	if err == nil {
		t.Fatalf("expected missing client id validation error")
	}
}

func TestOAuth2Provider_BeginAuth_GeneratesRandomStateWhenMissing(t *testing.T) {
	provider, err := NewOAuth2Provider(OAuth2Config{
		ID:            "github",
		AuthURL:       "https://github.com/login/oauth/authorize",
		TokenURL:      "https://github.com/login/oauth/access_token",
		ClientID:      "client-123",
		ClientSecret:  "secret-456",
		DefaultScopes: []string{"repo"},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	first, err := provider.BeginAuth(context.Background(), core.BeginAuthRequest{
		Scope: core.ScopeRef{Type: "user", ID: "usr_1"},
	})
	if err != nil {
		t.Fatalf("begin auth first: %v", err)
	}
	second, err := provider.BeginAuth(context.Background(), core.BeginAuthRequest{
		Scope: core.ScopeRef{Type: "user", ID: "usr_1"},
	})
	if err != nil {
		t.Fatalf("begin auth second: %v", err)
	}

	if strings.TrimSpace(first.State) == "" || strings.TrimSpace(second.State) == "" {
		t.Fatalf("expected generated oauth states")
	}
	if first.State == second.State {
		t.Fatalf("expected generated states to differ")
	}
	if strings.HasPrefix(first.State, "state_github_") || strings.HasPrefix(second.State, "state_github_") {
		t.Fatalf("expected cryptographically generated state, not deterministic fallback format")
	}
}

func TestOAuth2Provider_CompleteAuth_ResolvesExternalAccountIDWithProfileResolver(t *testing.T) {
	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Authorization")) != "Bearer access_123" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":   "google-sub-1",
			"email": "user@example.com",
		})
	}))
	defer userInfoServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access_123",
			"token_type":   "Bearer",
			"expires_in":   1800,
		})
	}))
	defer tokenServer.Close()

	provider, err := NewOAuth2Provider(OAuth2Config{
		ID:           "google_docs",
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     tokenServer.URL,
		ClientID:     "client-123",
		ClientSecret: "secret-456",
		ProfileResolver: identity.NewResolver(identity.Config{
			ProviderUserInfo: map[string]identity.ProviderUserInfoConfig{
				"google_docs": {
					URL:    userInfoServer.URL,
					Issuer: "https://accounts.google.com",
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
		Scope: core.ScopeRef{Type: "user", ID: "usr_1"},
		Code:  "code_123",
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if complete.ExternalAccountID != "https://accounts.google.com|google-sub-1" {
		t.Fatalf("expected resolved external account id, got %q", complete.ExternalAccountID)
	}
}

func TestOAuth2Provider_CompleteAuth_PersistsIDTokenInMetadata(t *testing.T) {
	idToken := mustJWTToken(map[string]any{
		"iss": "https://accounts.google.com",
		"sub": "sub_1",
	})
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access_123",
			"id_token":     idToken,
			"token_type":   "Bearer",
			"expires_in":   1800,
		})
	}))
	defer tokenServer.Close()

	provider, err := NewOAuth2Provider(OAuth2Config{
		ID:           "google_docs",
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     tokenServer.URL,
		ClientID:     "client-123",
		ClientSecret: "secret-456",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
		Scope:    core.ScopeRef{Type: "user", ID: "usr_1"},
		Code:     "code_123",
		Metadata: map[string]any{"external_account_id": "acct_1"},
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	credentialIDToken, _ := complete.Credential.Metadata["id_token"].(string)
	if credentialIDToken != idToken {
		t.Fatalf("expected id_token in credential metadata")
	}
	responseIDToken, _ := complete.Metadata["id_token"].(string)
	if responseIDToken != idToken {
		t.Fatalf("expected id_token in response metadata")
	}
}

func mustJWTToken(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + encodedPayload + ".signature"
}
