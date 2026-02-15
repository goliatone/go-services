package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
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
		Scope: core.ScopeRef{Type: "user", ID: "usr_1"},
		Code:  "code_123",
		State: begin.State,
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if complete.ExternalAccountID == "" {
		t.Fatalf("expected external account id")
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
