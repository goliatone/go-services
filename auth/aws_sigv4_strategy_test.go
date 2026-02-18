package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestOAuth2SigV4Strategy_CompleteAndRefresh(t *testing.T) {
	now := time.Date(2026, 2, 18, 10, 0, 0, 0, time.UTC)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "oauth_token_1",
			"token_type":   "bearer",
			"expires_in":   1800,
			"scope":        "orders:read",
		})
	}))
	defer tokenServer.Close()

	strategy := NewOAuth2SigV4Strategy(OAuth2SigV4StrategyConfig{
		OAuth2: OAuth2ClientCredentialsStrategyConfig{
			ClientID:     "client_1",
			ClientSecret: "secret_1",
			TokenURL:     tokenServer.URL,
			Now:          func() time.Time { return now },
		},
		Profile: AWSSigV4SigningProfile{
			AccessKeyID:       "AKIA_TEST",
			SecretAccessKey:   "secret_key",
			SessionToken:      "session_token",
			Region:            "us-east-1",
			Service:           "execute-api",
			SigningMode:       "header",
			AccessTokenHeader: "x-amz-access-token",
		},
	})

	complete, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_1"},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if strategy.Type() != core.AuthKindAWSSigV4 {
		t.Fatalf("unexpected strategy type: %s", strategy.Type())
	}
	if got := complete.Metadata["auth_kind"]; got != core.AuthKindAWSSigV4 {
		t.Fatalf("expected auth kind metadata %q, got %v", core.AuthKindAWSSigV4, got)
	}
	if got := complete.Credential.Metadata["aws_region"]; got != "us-east-1" {
		t.Fatalf("expected aws_region metadata, got %v", got)
	}
	if got := complete.Credential.Metadata["aws_service"]; got != "execute-api" {
		t.Fatalf("expected aws_service metadata, got %v", got)
	}

	refreshed, err := strategy.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := refreshed.Credential.Metadata["auth_kind"]; got != core.AuthKindAWSSigV4 {
		t.Fatalf("expected refreshed auth kind metadata %q, got %v", core.AuthKindAWSSigV4, got)
	}
}

func TestOAuth2SigV4Strategy_RequiresSigV4ProfileMetadata(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "oauth_token_2",
			"token_type":   "bearer",
			"expires_in":   1200,
		})
	}))
	defer tokenServer.Close()

	strategy := NewOAuth2SigV4Strategy(OAuth2SigV4StrategyConfig{
		OAuth2: OAuth2ClientCredentialsStrategyConfig{
			ClientID:     "client_2",
			ClientSecret: "secret_2",
			TokenURL:     tokenServer.URL,
		},
	})

	_, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_2"},
	})
	if err == nil {
		t.Fatalf("expected missing sigv4 profile validation error")
	}
}
