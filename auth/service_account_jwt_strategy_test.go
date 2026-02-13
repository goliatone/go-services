package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestServiceAccountJWTStrategy_CompleteAndRefresh(t *testing.T) {
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	privateKeyPEM := generateTestRSAPrivateKeyPEM(t)
	strategy := NewServiceAccountJWTStrategy(ServiceAccountJWTStrategyConfig{
		Issuer:           "svc@example.iam.gserviceaccount.com",
		Audience:         "https://oauth2.googleapis.com/token",
		Subject:          "svc@example.iam.gserviceaccount.com",
		SigningAlgorithm: "RS256",
		SigningKey:       privateKeyPEM,
		KeyID:            "kid-1",
		TokenTTL:         30 * time.Minute,
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

func TestServiceAccountJWTStrategy_CompleteRejectsUnsupportedAlgorithm(t *testing.T) {
	strategy := NewServiceAccountJWTStrategy(ServiceAccountJWTStrategyConfig{
		Issuer:     "svc@example.iam.gserviceaccount.com",
		Audience:   "https://oauth2.googleapis.com/token",
		Subject:    "svc@example.iam.gserviceaccount.com",
		SigningKey: generateTestRSAPrivateKeyPEM(t),
	})

	_, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_1"},
		Metadata: map[string]any{
			"signing_algorithm": "ES256",
		},
	})
	if err == nil {
		t.Fatalf("expected unsupported algorithm error")
	}
}

func generateTestRSAPrivateKeyPEM(t *testing.T) string {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	encoded := x509.MarshalPKCS1PrivateKey(privateKey)
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: encoded,
	})
	if len(pemBlock) == 0 {
		t.Fatalf("encode rsa key to pem")
	}
	return string(pemBlock)
}
