package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestEmbeddedService_AuthenticateEmbedded_EndToEnd(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("requested_token_type"); got != requestedTypeOfflineURN {
			t.Fatalf("expected offline token type, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "offline_access_token_1",
			"token_type":   "bearer",
			"scope":        "read_products read_orders",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	svc, err := NewService(ServiceConfig{
		ClientID:     "client_id",
		ClientSecret: "app_secret",
		Now:          func() time.Time { return now },
		Exchanger: NewSessionTokenExchangeClient(ExchangeClientConfig{
			ClientID:     "client_id",
			ClientSecret: "app_secret",
			BuildTokenURL: func(_ string) (string, error) {
				return server.URL, nil
			},
			Now: func() time.Time { return now },
		}),
	})
	if err != nil {
		t.Fatalf("new embedded service: %v", err)
	}

	sessionToken := signTestJWT(t, "app_secret", map[string]any{
		"iss":  "https://merchant.myshopify.com/admin",
		"dest": "https://merchant.myshopify.com",
		"aud":  "client_id",
		"sub":  "user_1",
		"exp":  now.Add(time.Minute).Unix(),
		"nbf":  now.Add(-time.Minute).Unix(),
		"iat":  now.Add(-30 * time.Second).Unix(),
		"jti":  "jti_auth_1",
	})
	result, err := svc.AuthenticateEmbedded(context.Background(), core.EmbeddedAuthRequest{
		ProviderID:   "shopify",
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		SessionToken: sessionToken,
	})
	if err != nil {
		t.Fatalf("authenticate embedded: %v", err)
	}

	if result.ShopDomain != "merchant.myshopify.com" {
		t.Fatalf("expected merchant shop domain, got %q", result.ShopDomain)
	}
	if result.ExternalAccountID != "merchant.myshopify.com" {
		t.Fatalf("expected external account id to be shop domain, got %q", result.ExternalAccountID)
	}
	if result.Credential.AccessToken != "offline_access_token_1" {
		t.Fatalf("unexpected credential access token")
	}
	if _, ok := result.Credential.Metadata["access_token"]; ok {
		t.Fatalf("access_token must not be copied into credential metadata")
	}
	if _, ok := result.Token.Metadata["access_token"]; ok {
		t.Fatalf("access_token must not be copied into token metadata")
	}
}

func TestEmbeddedService_AuthenticateEmbedded_ReplayRejected(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token_1",
			"token_type":   "bearer",
		})
	}))
	defer server.Close()

	svc, err := NewService(ServiceConfig{
		ClientID:     "client_id",
		ClientSecret: "app_secret",
		Now:          func() time.Time { return now },
		Exchanger: NewSessionTokenExchangeClient(ExchangeClientConfig{
			ClientID:     "client_id",
			ClientSecret: "app_secret",
			BuildTokenURL: func(_ string) (string, error) {
				return server.URL, nil
			},
			Now: func() time.Time { return now },
		}),
	})
	if err != nil {
		t.Fatalf("new embedded service: %v", err)
	}

	sessionToken := signTestJWT(t, "app_secret", map[string]any{
		"iss":  "https://merchant.myshopify.com/admin",
		"dest": "https://merchant.myshopify.com",
		"aud":  "client_id",
		"exp":  now.Add(time.Minute).Unix(),
		"nbf":  now.Add(-time.Minute).Unix(),
		"iat":  now.Add(-30 * time.Second).Unix(),
		"jti":  "jti_replay_1",
	})
	if _, err := svc.AuthenticateEmbedded(context.Background(), core.EmbeddedAuthRequest{
		ProviderID:   "shopify",
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		SessionToken: sessionToken,
	}); err != nil {
		t.Fatalf("authenticate first: %v", err)
	}

	if _, err := svc.AuthenticateEmbedded(context.Background(), core.EmbeddedAuthRequest{
		ProviderID:   "shopify",
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		SessionToken: sessionToken,
	}); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("expected replay error, got %v", err)
	}
}

func TestEmbeddedService_AuthenticateEmbedded_FailedExchangeStillConsumesJTI(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "exchange_failed",
			"error_description": "upstream unavailable",
		})
	}))
	defer server.Close()

	svc, err := NewService(ServiceConfig{
		ClientID:     "client_id",
		ClientSecret: "app_secret",
		Now:          func() time.Time { return now },
		Exchanger: NewSessionTokenExchangeClient(ExchangeClientConfig{
			ClientID:     "client_id",
			ClientSecret: "app_secret",
			BuildTokenURL: func(_ string) (string, error) {
				return server.URL, nil
			},
			Now: func() time.Time { return now },
		}),
	})
	if err != nil {
		t.Fatalf("new embedded service: %v", err)
	}
	sessionToken := signTestJWT(t, "app_secret", map[string]any{
		"iss":  "https://merchant.myshopify.com/admin",
		"dest": "https://merchant.myshopify.com",
		"aud":  "client_id",
		"exp":  now.Add(time.Minute).Unix(),
		"nbf":  now.Add(-time.Minute).Unix(),
		"iat":  now.Add(-30 * time.Second).Unix(),
		"jti":  "jti_exchange_failure_1",
	})

	if _, err := svc.AuthenticateEmbedded(context.Background(), core.EmbeddedAuthRequest{
		ProviderID:   "shopify",
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		SessionToken: sessionToken,
	}); err == nil {
		t.Fatalf("expected exchange error")
	}

	if _, err := svc.AuthenticateEmbedded(context.Background(), core.EmbeddedAuthRequest{
		ProviderID:   "shopify",
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		SessionToken: sessionToken,
	}); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("expected replay error after failed exchange, got %v", err)
	}
}

func TestEmbeddedService_AuthenticateEmbedded_InvalidRequestedTokenTypeRejected(t *testing.T) {
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	exchanger := &exchangerStub{}
	svc, err := NewService(ServiceConfig{
		ClientID:     "client_id",
		ClientSecret: "app_secret",
		Now:          func() time.Time { return now },
		Exchanger:    exchanger,
	})
	if err != nil {
		t.Fatalf("new embedded service: %v", err)
	}
	sessionToken := signTestJWT(t, "app_secret", map[string]any{
		"iss":  "https://merchant.myshopify.com/admin",
		"dest": "https://merchant.myshopify.com",
		"aud":  "client_id",
		"exp":  now.Add(time.Minute).Unix(),
		"nbf":  now.Add(-time.Minute).Unix(),
		"iat":  now.Add(-30 * time.Second).Unix(),
		"jti":  "jti_invalid_type_1",
	})

	_, err = svc.AuthenticateEmbedded(context.Background(), core.EmbeddedAuthRequest{
		ProviderID:         "shopify",
		Scope:              core.ScopeRef{Type: "org", ID: "org_1"},
		SessionToken:       sessionToken,
		RequestedTokenType: core.EmbeddedRequestedTokenType("invalid"),
	})
	if err == nil {
		t.Fatalf("expected invalid requested token type error")
	}
	if !errors.Is(err, ErrInvalidRequestedTokenType) {
		t.Fatalf("expected ErrInvalidRequestedTokenType, got %v", err)
	}
	if exchanger.calls != 0 {
		t.Fatalf("exchanger should not be called for invalid requested token type")
	}

	if _, retryErr := svc.AuthenticateEmbedded(context.Background(), core.EmbeddedAuthRequest{
		ProviderID:   "shopify",
		Scope:        core.ScopeRef{Type: "org", ID: "org_1"},
		SessionToken: sessionToken,
	}); retryErr != nil {
		t.Fatalf("expected valid retry to succeed after invalid token type: %v", retryErr)
	}
}

type exchangerStub struct {
	calls int
}

func (s *exchangerStub) ExchangeSessionToken(
	_ context.Context,
	_ ExchangeSessionTokenRequest,
) (core.EmbeddedAccessToken, error) {
	s.calls++
	return core.EmbeddedAccessToken{
		AccessToken: "unused",
		TokenType:   "bearer",
	}, nil
}
