package core

import (
	"context"
	"net/http"
	"testing"
)

func TestSignRequest_UsesDefaultBearerSigner(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	credentialStore := newMemoryCredentialStore()
	if _, err := credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
		ConnectionID:     "conn_1",
		EncryptedPayload: []byte("token-123"),
		TokenType:        "bearer",
		Status:           CredentialStatusActive,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithCredentialStore(credentialStore),
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
