package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestOAuth2ClientCredentialsStrategy_CacheAndRenew(t *testing.T) {
	now := time.Date(2026, 2, 13, 15, 0, 0, 0, time.UTC)
	requests := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			http.Error(w, "unexpected grant_type", http.StatusBadRequest)
			return
		}
		if r.Form.Get("scope") != "repo:read" {
			http.Error(w, "unexpected scope", http.StatusBadRequest)
			return
		}
		clientID, clientSecret, ok := r.BasicAuth()
		if !ok || clientID != "client_1" || clientSecret != "secret_1" {
			http.Error(w, "invalid client auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("cc_token_%d", requests),
			"token_type":   "bearer",
			"expires_in":   3600,
			"scope":        "repo:read",
		})
	}))
	defer tokenServer.Close()

	strategy := NewOAuth2ClientCredentialsStrategy(OAuth2ClientCredentialsStrategyConfig{
		ClientID:      "client_1",
		ClientSecret:  "secret_1",
		TokenURL:      tokenServer.URL,
		DefaultScopes: []string{"repo:read"},
		TokenTTL:      time.Hour,
		RenewBefore:   2 * time.Minute,
		Now: func() time.Time {
			return now
		},
	})

	req := core.AuthCompleteRequest{Scope: core.ScopeRef{Type: "org", ID: "o1"}}

	first, err := strategy.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete first: %v", err)
	}
	second, err := strategy.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete second: %v", err)
	}
	if first.Credential.AccessToken != second.Credential.AccessToken {
		t.Fatalf("expected cached token reuse")
	}
	if requests != 1 {
		t.Fatalf("expected exactly one token endpoint call while cached, got %d", requests)
	}

	now = now.Add(59 * time.Minute)
	third, err := strategy.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete third: %v", err)
	}
	if third.Credential.AccessToken == second.Credential.AccessToken {
		t.Fatalf("expected token renewal near expiry")
	}
	if requests != 2 {
		t.Fatalf("expected second token endpoint call after renewal window, got %d", requests)
	}
}

func TestOAuth2ClientCredentialsStrategy_Refresh(t *testing.T) {
	now := time.Date(2026, 2, 13, 16, 0, 0, 0, time.UTC)
	requests := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			http.Error(w, "unexpected grant_type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("refresh_token_%d", requests),
			"token_type":   "bearer",
			"expires_in":   900,
			"scope":        "repo:read",
		})
	}))
	defer tokenServer.Close()

	strategy := NewOAuth2ClientCredentialsStrategy(OAuth2ClientCredentialsStrategyConfig{
		ClientID:      "client_refresh",
		ClientSecret:  "secret_refresh",
		TokenURL:      tokenServer.URL,
		DefaultScopes: []string{"repo:read"},
		TokenTTL:      15 * time.Minute,
		Now: func() time.Time {
			return now
		},
	})

	complete, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "user", ID: "u1"},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected one initial token call, got %d", requests)
	}

	now = now.Add(1 * time.Minute)
	refreshed, err := strategy.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Credential.AccessToken == complete.Credential.AccessToken {
		t.Fatalf("expected refreshed token")
	}
	if refreshed.Credential.ExpiresAt == nil || !refreshed.Credential.ExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("unexpected refreshed token expiry")
	}
	if requests != 2 {
		t.Fatalf("expected refresh to call token endpoint, got %d calls", requests)
	}
}

func TestOAuth2ClientCredentialsStrategy_CompleteRequiresCredentials(t *testing.T) {
	strategy := NewOAuth2ClientCredentialsStrategy(OAuth2ClientCredentialsStrategyConfig{})
	_, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "user", ID: "u2"},
	})
	if err == nil {
		t.Fatalf("expected credential config error")
	}
}

func TestOAuth2ClientCredentialsStrategy_CompleteRequiresTokenURL(t *testing.T) {
	strategy := NewOAuth2ClientCredentialsStrategy(OAuth2ClientCredentialsStrategyConfig{
		ClientID:     "client",
		ClientSecret: "secret",
	})
	_, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "user", ID: "u3"},
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "token_url") {
		t.Fatalf("expected token_url validation error, got %v", err)
	}
}
