package auth

import (
	"context"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestAPIKeyStrategy_Complete(t *testing.T) {
	strategy := NewAPIKeyStrategy(APIKeyStrategyConfig{})

	result, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "user", ID: "u1"},
		Metadata: map[string]any{
			"api_key":          "key_123",
			"requested_grants": []string{"repo:read"},
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if result.Credential.TokenType != core.AuthKindAPIKey {
		t.Fatalf("expected token type %q, got %q", core.AuthKindAPIKey, result.Credential.TokenType)
	}
	if result.Credential.AccessToken != "key_123" {
		t.Fatalf("unexpected access token")
	}
	if got := result.Credential.Metadata["api_key_header"]; got != defaultAPIKeyHeader {
		t.Fatalf("expected default api key header, got %v", got)
	}
}

func TestPATStrategy_DefaultProfile(t *testing.T) {
	strategy := NewPATStrategy(APIKeyStrategyConfig{})
	if strategy.Type() != core.AuthKindPAT {
		t.Fatalf("expected pat type")
	}

	result, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "user", ID: "u2"},
		Metadata: map[string]any{
			"pat": "pat_value",
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := result.Credential.Metadata["api_key_header"]; got != "Authorization" {
		t.Fatalf("expected Authorization header for PAT, got %v", got)
	}
	if got := result.Credential.Metadata["api_key_prefix"]; got != "token" {
		t.Fatalf("expected token prefix for PAT, got %v", got)
	}
}

func TestHMACStrategy_Complete(t *testing.T) {
	strategy := NewHMACStrategy(HMACStrategyConfig{})

	result, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_1"},
		Metadata: map[string]any{
			"hmac_secret":     "secret_value",
			"hmac_key_id":     "key_1",
			"requested_scope": "unused",
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if result.Credential.TokenType != core.AuthKindHMAC {
		t.Fatalf("expected hmac token type")
	}
	if result.Credential.AccessToken != "secret_value" {
		t.Fatalf("unexpected hmac secret")
	}
	if got := result.Credential.Metadata["hmac_key_id"]; got != "key_1" {
		t.Fatalf("expected hmac key id metadata, got %v", got)
	}
}

func TestHMACStrategy_CompleteRequiresSecret(t *testing.T) {
	strategy := NewHMACStrategy(HMACStrategyConfig{})
	_, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "user", ID: "u3"},
	})
	if err == nil {
		t.Fatalf("expected missing secret error")
	}
}
