package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
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

func TestServiceAccountJWTStrategy_RefreshUsesMetadataSigningKey(t *testing.T) {
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	privateKeyPEM := generateTestRSAPrivateKeyPEM(t)
	strategy := NewServiceAccountJWTStrategy(ServiceAccountJWTStrategyConfig{
		Issuer:   "svc@example.iam.gserviceaccount.com",
		Audience: "https://oauth2.googleapis.com/token",
		TokenTTL: 30 * time.Minute,
		Now: func() time.Time {
			return now
		},
	})

	complete, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "svc@example.iam.gserviceaccount.com"},
		Metadata: map[string]any{
			"signing_key": privateKeyPEM,
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := strings.TrimSpace(readString(complete.Credential.Metadata, "signing_key")); got == "" {
		t.Fatalf("expected metadata signing key to be preserved for refresh")
	}

	now = now.Add(10 * time.Minute)
	refreshed, err := strategy.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Credential.AccessToken == complete.Credential.AccessToken {
		t.Fatalf("expected refreshed jwt token")
	}
}

func TestServiceAccountJWTStrategy_GoogleServiceAccountJSONExchangesAssertion(t *testing.T) {
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	privateKeyPEM := generateTestRSAPrivateKeyPEM(t)
	var observedAssertion string
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST token exchange, got %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/x-www-form-urlencoded") {
			t.Fatalf("expected form content type, got %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatalf("expected jwt bearer grant, got %q", got)
		}
		observedAssertion = r.Form.Get("assertion")
		if strings.TrimSpace(observedAssertion) == "" {
			t.Fatalf("expected signed assertion")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"oauth-access-token","token_type":"Bearer","expires_in":3600,"scope":"https://www.googleapis.com/auth/drive.readonly"}`))
	}))
	defer tokenEndpoint.Close()

	serviceAccountJSON, err := json.Marshal(map[string]string{
		"client_email":   "svc@example.iam.gserviceaccount.com",
		"private_key":    privateKeyPEM,
		"private_key_id": "kid-1",
		"token_uri":      tokenEndpoint.URL,
		"project_id":     "project-1",
	})
	if err != nil {
		t.Fatalf("marshal service account json: %v", err)
	}
	strategy := NewServiceAccountJWTStrategy(ServiceAccountJWTStrategyConfig{
		TokenTTL: 30 * time.Minute,
		Now: func() time.Time {
			return now
		},
		HTTPClient: tokenEndpoint.Client(),
	})

	complete, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_1"},
		Metadata: map[string]any{
			"service_account_json":    string(serviceAccountJSON),
			"service_account_subject": "delegate@example.org",
			"requested_grants":        []string{"https://www.googleapis.com/auth/drive.readonly"},
		},
	})
	if err != nil {
		t.Fatalf("complete google service account: %v", err)
	}
	if complete.Credential.AccessToken != "oauth-access-token" {
		t.Fatalf("expected OAuth access token from endpoint, got %q", complete.Credential.AccessToken)
	}
	if strings.Contains(complete.Credential.AccessToken, ".") {
		t.Fatalf("expected access token not assertion jwt")
	}
	if complete.Credential.ExpiresAt == nil || !complete.Credential.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected expiry: %v", complete.Credential.ExpiresAt)
	}
	claims := decodeJWTClaimsForTest(t, observedAssertion)
	if claims["iss"] != "svc@example.iam.gserviceaccount.com" {
		t.Fatalf("unexpected assertion issuer: %#v", claims["iss"])
	}
	if claims["sub"] != "delegate@example.org" {
		t.Fatalf("expected delegated subject, got %#v", claims["sub"])
	}
	if claims["scope"] != "https://www.googleapis.com/auth/drive.readonly" {
		t.Fatalf("unexpected assertion scope: %#v", claims["scope"])
	}

	redacted := core.RedactSensitiveMap(complete.Credential.Metadata)
	if redacted["signing_key"] != core.RedactedValue {
		t.Fatalf("expected private key signing metadata redacted, got %#v", redacted["signing_key"])
	}

	now = now.Add(10 * time.Minute)
	refreshed, err := strategy.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatalf("refresh google service account: %v", err)
	}
	if refreshed.Credential.AccessToken != "oauth-access-token" {
		t.Fatalf("expected refreshed OAuth access token from endpoint, got %q", refreshed.Credential.AccessToken)
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

func decodeJWTClaimsForTest(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected jwt assertion, got %q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode jwt claims: %v", err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshal jwt claims: %v", err)
	}
	return claims
}
