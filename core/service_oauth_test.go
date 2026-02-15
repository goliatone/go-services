package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestConnectAndCompleteCallback_ConsumesOAuthState(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	stateStore := NewMemoryOAuthStateStore(time.Minute)
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(stateStore),
		WithConnectionStore(newMemoryConnectionStore()),
		WithCredentialStore(newMemoryCredentialStore()),
		WithSecretProvider(testSecretProvider{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connectResp, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:      "github",
		Scope:           ScopeRef{Type: "user", ID: "u1"},
		RedirectURI:     "https://app.example/callback",
		RequestedGrants: []string{"repo:read"},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if strings.TrimSpace(connectResp.State) == "" {
		t.Fatalf("expected callback state")
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u1"},
		Code:        "code-1",
		State:       connectResp.State,
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("complete callback: %v", err)
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u1"},
		Code:        "code-2",
		State:       connectResp.State,
		RedirectURI: "https://app.example/callback",
	})
	if err == nil || !strings.Contains(err.Error(), "oauth state not found") {
		t.Fatalf("expected consumed state error, got %v", err)
	}
}

func TestCompleteCallback_RejectsMismatchedStateContextBeforeProviderCall(t *testing.T) {
	ctx := context.Background()

	provider := &spyProvider{testProvider: testProvider{id: "github"}}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	stateStore := NewMemoryOAuthStateStore(time.Minute)
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(stateStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connectResp, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u1"},
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "org", ID: "o1"},
		Code:        "code",
		State:       connectResp.State,
		RedirectURI: "https://app.example/callback",
	})
	if err == nil || !strings.Contains(err.Error(), "state scope mismatch") {
		t.Fatalf("expected scope mismatch error, got %v", err)
	}
	if provider.completeCalls != 0 {
		t.Fatalf("expected provider callback not to be called on state mismatch")
	}
}

func TestCompleteCallback_RequiresSecretProviderForCredentialPersistence(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	stateStore := NewMemoryOAuthStateStore(time.Minute)
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(stateStore),
		WithConnectionStore(newMemoryConnectionStore()),
		WithCredentialStore(newMemoryCredentialStore()),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connectResp, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u2"},
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u2"},
		Code:        "code",
		State:       connectResp.State,
		RedirectURI: "https://app.example/callback",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "secret provider is required") {
		t.Fatalf("expected secret provider enforcement error, got %v", err)
	}
}

func TestCompleteCallback_UsesStateContextWhenCallbackOmitsRedirectAndMetadata(t *testing.T) {
	ctx := context.Background()

	provider := &spyProvider{testProvider: testProvider{id: "github"}}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(NewMemoryOAuthStateStore(time.Minute)),
		WithConnectionStore(newMemoryConnectionStore()),
		WithCredentialStore(newMemoryCredentialStore()),
		WithSecretProvider(testSecretProvider{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connectResp, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:      "github",
		Scope:           ScopeRef{Type: "user", ID: "u3"},
		RedirectURI:     "https://app.example/callback",
		RequestedGrants: []string{"repo:read"},
		Metadata:        map[string]any{"source": "connect"},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "user", ID: "u3"},
		Code:       "code",
		State:      connectResp.State,
	})
	if err != nil {
		t.Fatalf("complete callback: %v", err)
	}
	if provider.lastCompleteAuthRequest.RedirectURI != "https://app.example/callback" {
		t.Fatalf("expected redirect uri to be restored from oauth state")
	}
	requestedRaw, ok := provider.lastCompleteAuthRequest.Metadata["requested_grants"]
	if !ok {
		t.Fatalf("expected requested grants in callback metadata")
	}
	requested, ok := requestedRaw.([]string)
	if !ok || len(requested) == 0 || requested[0] != "repo:read" {
		t.Fatalf("expected requested grants to be restored from oauth state metadata, got %#v", requestedRaw)
	}
}

func TestCompleteCallback_RedirectValidationIsConfigurable(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(
		Config{
			OAuth: OAuthConfig{RequireCallbackRedirect: true},
		},
		WithRegistry(registry),
		WithOAuthStateStore(NewMemoryOAuthStateStore(time.Minute)),
		WithConnectionStore(newMemoryConnectionStore()),
		WithCredentialStore(newMemoryCredentialStore()),
		WithSecretProvider(testSecretProvider{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connectResp, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u4"},
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "user", ID: "u4"},
		Code:       "code",
		State:      connectResp.State,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "redirect uri is required") {
		t.Fatalf("expected strict redirect validation error, got %v", err)
	}

	connectResp, err = svc.Connect(ctx, ConnectRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u5"},
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "user", ID: "u5"},
		Code:       "code",
		State:      connectResp.State,
		Metadata: map[string]any{
			"require_callback_redirect": false,
		},
	})
	if err != nil {
		t.Fatalf("expected metadata override to relax strict redirect validation: %v", err)
	}
}

type spyProvider struct {
	testProvider
	completeCalls           int
	lastCompleteAuthRequest CompleteAuthRequest
}

func (s *spyProvider) CompleteAuth(ctx context.Context, req CompleteAuthRequest) (CompleteAuthResponse, error) {
	s.completeCalls++
	s.lastCompleteAuthRequest = req
	return s.testProvider.CompleteAuth(ctx, req)
}
