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
