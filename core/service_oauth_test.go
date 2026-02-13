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

type spyProvider struct {
	testProvider
	completeCalls int
}

func (s *spyProvider) CompleteAuth(ctx context.Context, req CompleteAuthRequest) (CompleteAuthResponse, error) {
	s.completeCalls++
	return s.testProvider.CompleteAuth(ctx, req)
}

