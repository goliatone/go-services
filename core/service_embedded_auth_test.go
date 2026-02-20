package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	goerrors "github.com/goliatone/go-errors"
)

func TestService_AuthenticateEmbedded_UsesProviderImplementation(t *testing.T) {
	registry := NewProviderRegistry()
	provider := &embeddedProviderStub{
		id: "shopify",
		result: EmbeddedAuthResult{
			ProviderID:        "shopify",
			Scope:             ScopeRef{Type: "org", ID: "org_1"},
			ShopDomain:        "merchant.myshopify.com",
			ExternalAccountID: "merchant.myshopify.com",
			Credential: ActiveCredential{
				TokenType:   "bearer",
				AccessToken: "offline_token_1",
			},
		},
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(Config{}, WithRegistry(registry))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.AuthenticateEmbedded(context.Background(), EmbeddedAuthRequest{
		ProviderID:   "shopify",
		Scope:        ScopeRef{Type: "org", ID: "org_1"},
		SessionToken: "session_token",
	})
	if err != nil {
		t.Fatalf("authenticate embedded: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider embedded auth to be called once, got %d", provider.calls)
	}
	if result.ShopDomain != "merchant.myshopify.com" {
		t.Fatalf("unexpected shop domain: %q", result.ShopDomain)
	}
}

func TestService_AuthenticateEmbedded_ReturnsUnsupportedWhenProviderDoesNotImplement(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(Config{}, WithRegistry(registry))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.AuthenticateEmbedded(context.Background(), EmbeddedAuthRequest{
		ProviderID:   "github",
		Scope:        ScopeRef{Type: "org", ID: "org_1"},
		SessionToken: "session_token",
	})
	if err == nil {
		t.Fatalf("expected unsupported embedded auth error")
	}
	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.TextCode != ServiceErrorEmbeddedAuthUnsupported {
		t.Fatalf("expected %s, got %s", ServiceErrorEmbeddedAuthUnsupported, rich.TextCode)
	}
}

type embeddedProviderStub struct {
	id     string
	calls  int
	result EmbeddedAuthResult
	err    error
}

func (p *embeddedProviderStub) ID() string { return p.id }

func (*embeddedProviderStub) AuthKind() AuthKind { return AuthKindOAuth2AuthCode }

func (*embeddedProviderStub) SupportedScopeTypes() []string { return []string{"org"} }

func (*embeddedProviderStub) Capabilities() []CapabilityDescriptor { return []CapabilityDescriptor{} }

func (*embeddedProviderStub) BeginAuth(_ context.Context, _ BeginAuthRequest) (BeginAuthResponse, error) {
	return BeginAuthResponse{}, nil
}

func (*embeddedProviderStub) CompleteAuth(_ context.Context, _ CompleteAuthRequest) (CompleteAuthResponse, error) {
	return CompleteAuthResponse{}, nil
}

func (*embeddedProviderStub) Refresh(_ context.Context, _ ActiveCredential) (RefreshResult, error) {
	return RefreshResult{}, nil
}

func (p *embeddedProviderStub) AuthenticateEmbedded(
	_ context.Context,
	req EmbeddedAuthRequest,
) (EmbeddedAuthResult, error) {
	p.calls++
	if p.err != nil {
		return EmbeddedAuthResult{}, p.err
	}
	out := p.result
	if out.ProviderID == "" {
		out.ProviderID = p.id
	}
	if out.Scope == (ScopeRef{}) {
		out.Scope = req.Scope
	}
	return out, nil
}

var _ Provider = (*embeddedProviderStub)(nil)
var _ EmbeddedAuthProvider = (*embeddedProviderStub)(nil)

func TestService_AuthenticateEmbedded_MapsProviderErrors(t *testing.T) {
	registry := NewProviderRegistry()
	provider := &embeddedProviderStub{
		id:  "shopify",
		err: fmt.Errorf("embedded auth replay detected"),
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	svc, err := NewService(Config{}, WithRegistry(registry))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.AuthenticateEmbedded(context.Background(), EmbeddedAuthRequest{
		ProviderID:   "shopify",
		Scope:        ScopeRef{Type: "org", ID: "org_1"},
		SessionToken: "session_token",
		ReplayTTL:    time.Minute,
	})
	if err == nil {
		t.Fatalf("expected replay error")
	}
}
