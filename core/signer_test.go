package core

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignRequest_UsesDefaultBearerSigner(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	credentialStore := newMemoryCredentialStore()
	encryptedToken, err := testSecretProvider{}.Encrypt(ctx, []byte("token-123"))
	if err != nil {
		t.Fatalf("encrypt seed credential: %v", err)
	}
	if _, err := credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
		ConnectionID:     "conn_1",
		EncryptedPayload: encryptedToken,
		TokenType:        "bearer",
		Status:           CredentialStatusActive,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithCredentialStore(credentialStore),
		WithSecretProvider(testSecretProvider{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example/resource", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := svc.SignRequest(ctx, "github", "conn_1", req, nil); err != nil {
		t.Fatalf("sign request: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token-123" {
		t.Fatalf("expected bearer authorization header, got %q", got)
	}
}

func TestSignRequest_UsesProviderSignerOverride(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(providerWithSigner{
		testProvider: testProvider{id: "github"},
		signer:       staticSigner{header: "X-Signed-By", value: "provider"},
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(Config{}, WithRegistry(registry))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.example/resource", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	cred := &ActiveCredential{AccessToken: "token-override"}
	if err := svc.SignRequest(ctx, "github", "conn_2", req, cred); err != nil {
		t.Fatalf("sign request: %v", err)
	}
	if got := req.Header.Get("X-Signed-By"); got != "provider" {
		t.Fatalf("expected provider signer header, got %q", got)
	}
}

func TestSignRequest_RejectsProviderMismatchForConnection(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register github provider: %v", err)
	}
	if err := registry.Register(testProvider{id: "slack"}); err != nil {
		t.Fatalf("register slack provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u_mismatch"},
		ExternalAccountID: "acct",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example/resource", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	err = svc.SignRequest(ctx, "slack", connection.ID, req, &ActiveCredential{AccessToken: "token"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "provider mismatch") {
		t.Fatalf("expected provider mismatch error, got %v", err)
	}
}

func TestAPIKeySigner_SetsHeaderAndQuery(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.example/resource", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	signer := APIKeySigner{
		Header:     "X-API-Key",
		QueryParam: "api_key",
	}
	if err := signer.Sign(context.Background(), req, ActiveCredential{AccessToken: "k_test"}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if got := req.Header.Get("X-API-Key"); got != "k_test" {
		t.Fatalf("expected api key header, got %q", got)
	}
	if got := req.URL.Query().Get("api_key"); got != "k_test" {
		t.Fatalf("expected api key query param, got %q", got)
	}
}

func TestPATSigner_SetsAuthorizationHeader(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.example/resource", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := (PATSigner{}).Sign(context.Background(), req, ActiveCredential{AccessToken: "pat_123"}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "token pat_123" {
		t.Fatalf("expected pat authorization header, got %q", got)
	}
}

func TestHMACSigner_SetsSignatureHeadersAndPreservesBody(t *testing.T) {
	body := []byte(`{"ok":true}`)
	req, err := http.NewRequest(http.MethodPost, "https://api.example/resource", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	signer := HMACSigner{
		SignatureHeader: "X-Signature",
		TimestampHeader: "X-Timestamp",
		Now: func() time.Time {
			return time.Unix(1739443200, 0).UTC()
		},
	}
	if err := signer.Sign(context.Background(), req, ActiveCredential{AccessToken: "secret"}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if req.Header.Get("X-Timestamp") == "" {
		t.Fatalf("expected timestamp header")
	}
	if req.Header.Get("X-Signature") == "" {
		t.Fatalf("expected signature header")
	}

	restoredBody, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(restoredBody) != string(body) {
		t.Fatalf("expected body to be preserved after signing")
	}
}

func TestBasicAuthSigner_SetsAuthorizationHeader(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.example/resource", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := (BasicAuthSigner{}).Sign(context.Background(), req, ActiveCredential{AccessToken: "dXNlcjpwYXNz"}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Basic dXNlcjpwYXNz" {
		t.Fatalf("expected basic auth header, got %q", got)
	}
}

func TestMTLSSigner_NoOp(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.example/resource", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := (MTLSSigner{}).Sign(context.Background(), req, ActiveCredential{}); err != nil {
		t.Fatalf("sign: %v", err)
	}
}

func TestAWSSigV4Signer_HeaderMode(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://sellingpartnerapi-na.amazon.com/orders/v0/orders?MarketplaceIds=ATVPDKIKX0DER", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	signer := AWSSigV4Signer{
		Now: func() time.Time {
			return time.Date(2026, 2, 18, 15, 30, 0, 0, time.UTC)
		},
	}
	cred := ActiveCredential{
		AccessToken: "lwa-token",
		Metadata: map[string]any{
			"auth_kind":               AuthKindAWSSigV4,
			"aws_access_key_id":       "AKIAEXAMPLE",
			"aws_secret_access_key":   "secret_value",
			"aws_session_token":       "session_token",
			"aws_region":              "us-east-1",
			"aws_service":             "execute-api",
			"aws_signing_mode":        "header",
			"aws_access_token_header": "x-amz-access-token",
		},
	}

	if err := signer.Sign(context.Background(), req, cred); err != nil {
		t.Fatalf("sigv4 sign: %v", err)
	}
	authHeader := req.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("expected sigv4 authorization header, got %q", authHeader)
	}
	if got := req.Header.Get("X-Amz-Date"); got == "" {
		t.Fatalf("expected x-amz-date header")
	}
	if got := req.Header.Get("X-Amz-Content-Sha256"); got == "" {
		t.Fatalf("expected x-amz-content-sha256 header")
	}
	if got := req.Header.Get("X-Amz-Access-Token"); got != "lwa-token" {
		t.Fatalf("expected LWA access token header, got %q", got)
	}
}

func TestAWSSigV4Signer_QueryMode(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://sellingpartnerapi-na.amazon.com/orders/v0/orders", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	signer := AWSSigV4Signer{
		Now: func() time.Time {
			return time.Date(2026, 2, 18, 15, 45, 0, 0, time.UTC)
		},
	}
	cred := ActiveCredential{
		Metadata: map[string]any{
			"auth_kind":             AuthKindAWSSigV4,
			"aws_access_key_id":     "AKIAEXAMPLE",
			"aws_secret_access_key": "secret_value",
			"aws_region":            "us-west-2",
			"aws_service":           "execute-api",
			"aws_signing_mode":      "query",
			"aws_signing_expires":   "120",
		},
	}
	if err := signer.Sign(context.Background(), req, cred); err != nil {
		t.Fatalf("sigv4 query sign: %v", err)
	}
	query := req.URL.Query()
	if query.Get("X-Amz-Signature") == "" {
		t.Fatalf("expected query signature")
	}
	if query.Get("X-Amz-Credential") == "" {
		t.Fatalf("expected query credential")
	}
}

func TestSignRequest_ResolvesAuthKindSpecificSigner(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "amazon"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	svc, err := NewService(Config{}, WithRegistry(registry))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://sellingpartnerapi-na.amazon.com/orders/v0/orders", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	cred := &ActiveCredential{
		Metadata: map[string]any{
			"auth_kind":             AuthKindAWSSigV4,
			"aws_access_key_id":     "AKIAEXAMPLE",
			"aws_secret_access_key": "secret_value",
			"aws_region":            "us-east-1",
			"aws_service":           "execute-api",
			"aws_signing_mode":      "header",
		},
	}
	if err := svc.SignRequest(ctx, "amazon", "", req, cred); err != nil {
		t.Fatalf("sign request: %v", err)
	}
	if got := req.Header.Get("Authorization"); !strings.HasPrefix(got, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("expected auth-kind signer selection, got %q", got)
	}
}

type providerWithSigner struct {
	testProvider
	signer Signer
}

func (p providerWithSigner) Signer() Signer { return p.signer }

type staticSigner struct {
	header string
	value  string
}

func (s staticSigner) Sign(_ context.Context, req *http.Request, _ ActiveCredential) error {
	req.Header.Set(s.header, s.value)
	return nil
}
