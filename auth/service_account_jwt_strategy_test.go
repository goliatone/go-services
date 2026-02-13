package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestServiceAccountJWTStrategy_CompleteAndRefresh(t *testing.T) {
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	strategy := NewServiceAccountJWTStrategy(ServiceAccountJWTStrategyConfig{
		Issuer:     "svc@example.iam.gserviceaccount.com",
		Audience:   "https://oauth2.googleapis.com/token",
		Subject:    "svc@example.iam.gserviceaccount.com",
		PrivateKey: "secret_signing_key",
		KeyID:      "kid-1",
		TokenTTL:   30 * time.Minute,
		Now: func() time.Time {
			return now
		},
	})

	complete, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_1"},
		Metadata: map[string]any{
			"requested_grants": []string{"docs.read"},
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if complete.Credential.TokenType != "bearer" {
		t.Fatalf("expected bearer token type")
	}
	if !strings.Contains(complete.Credential.AccessToken, ".") {
		t.Fatalf("expected jwt token format")
	}
	if !complete.Credential.Refreshable {
		t.Fatalf("expected refreshable service account credential")
	}
	if complete.Credential.ExpiresAt == nil || !complete.Credential.ExpiresAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("unexpected expiry")
	}

	now = now.Add(10 * time.Minute)
	refreshed, err := strategy.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Credential.AccessToken == complete.Credential.AccessToken {
		t.Fatalf("expected refreshed jwt token")
	}
	if refreshed.Credential.ExpiresAt == nil || !refreshed.Credential.ExpiresAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("unexpected refreshed expiry")
	}
}

func TestServiceAccountJWTStrategy_CompleteRequiresConfig(t *testing.T) {
	strategy := NewServiceAccountJWTStrategy(ServiceAccountJWTStrategyConfig{})
	_, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "user", ID: "u1"},
	})
	if err == nil {
		t.Fatalf("expected required config error")
	}
}

