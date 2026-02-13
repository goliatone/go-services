package auth

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestOAuth2ClientCredentialsStrategy_CacheAndRenew(t *testing.T) {
	now := time.Date(2026, 2, 13, 15, 0, 0, 0, time.UTC)
	strategy := NewOAuth2ClientCredentialsStrategy(OAuth2ClientCredentialsStrategyConfig{
		ClientID:      "client_1",
		ClientSecret:  "secret_1",
		TokenURL:      "https://oauth.example/token",
		DefaultScopes: []string{"repo:read"},
		TokenTTL:      time.Hour,
		RenewBefore:   2 * time.Minute,
		Now: func() time.Time {
			return now
		},
	})

	req := core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "o1"},
	}

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

	now = now.Add(59 * time.Minute)
	third, err := strategy.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete third: %v", err)
	}
	if third.Credential.AccessToken == second.Credential.AccessToken {
		t.Fatalf("expected token renewal near expiry")
	}
}

func TestOAuth2ClientCredentialsStrategy_Refresh(t *testing.T) {
	now := time.Date(2026, 2, 13, 16, 0, 0, 0, time.UTC)
	strategy := NewOAuth2ClientCredentialsStrategy(OAuth2ClientCredentialsStrategyConfig{
		ClientID:      "client_refresh",
		ClientSecret:  "secret_refresh",
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

