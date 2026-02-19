package core

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	goerrors "github.com/goliatone/go-errors"
)

func TestService_UsesResolvedAuthStrategyForNonOAuthLifecycle(t *testing.T) {
	ctx := context.Background()

	strategy := &recordingAuthStrategy{
		kind: AuthKindAPIKey,
		beginResponse: AuthBeginResponse{
			URL:             "https://example.test/direct",
			RequestedGrants: []string{"repo:read"},
		},
		completeResponse: AuthCompleteResponse{
			ExternalAccountID: "acct_non_oauth",
			Credential: ActiveCredential{
				TokenType:       "api_key",
				AccessToken:     "api_key_value",
				RequestedScopes: []string{"repo:read"},
				GrantedScopes:   []string{"repo:read"},
				Refreshable:     true,
				Metadata:        map[string]any{"strategy": "api_key"},
			},
			RequestedGrants: []string{"repo:read"},
			GrantedGrants:   []string{"repo:read"},
		},
		refreshResponse: RefreshResult{
			Credential: ActiveCredential{
				TokenType:       "api_key",
				AccessToken:     "api_key_value_refreshed",
				RequestedScopes: []string{"repo:read"},
				GrantedScopes:   []string{"repo:read"},
				Refreshable:     true,
				Metadata:        map[string]any{"strategy": "api_key"},
			},
			GrantedGrants: []string{"repo:read"},
		},
	}

	registry := NewProviderRegistry()
	provider := &strategyProviderStub{
		id:       "custom_api",
		authKind: AuthKindAPIKey,
		strategy: strategy,
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	credentialStore := newMemoryCredentialStore()
	stateStore := NewMemoryOAuthStateStore(time.Minute)

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(stateStore),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
		WithSecretProvider(testSecretProvider{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	begin, err := svc.Connect(ctx, ConnectRequest{
		ProviderID: "custom_api",
		Scope:      ScopeRef{Type: "user", ID: "u_non_oauth"},
		Metadata:   map[string]any{"api_key": "api_key_value"},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if begin.State != "" {
		t.Fatalf("expected non-oauth strategy to avoid callback state generation, got %q", begin.State)
	}

	completion, err := svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID: "custom_api",
		Scope:      ScopeRef{Type: "user", ID: "u_non_oauth"},
		Metadata:   map[string]any{"api_key": "api_key_value"},
	})
	if err != nil {
		t.Fatalf("complete callback: %v", err)
	}
	if completion.Connection.ExternalAccountID != "acct_non_oauth" {
		t.Fatalf("unexpected external account id: %q", completion.Connection.ExternalAccountID)
	}
	if strategy.beginCalls != 1 || strategy.completeCalls != 1 {
		t.Fatalf("expected strategy begin/complete calls to be 1/1, got %d/%d", strategy.beginCalls, strategy.completeCalls)
	}
	if provider.beginCalls != 0 || provider.completeCalls != 0 || provider.refreshCalls != 0 {
		t.Fatalf("expected provider direct auth methods not to be used, got begin=%d complete=%d refresh=%d", provider.beginCalls, provider.completeCalls, provider.refreshCalls)
	}

	if _, err := svc.Refresh(ctx, RefreshRequest{
		ProviderID:   "custom_api",
		ConnectionID: completion.Connection.ID,
	}); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if strategy.refreshCalls != 1 {
		t.Fatalf("expected strategy refresh call count 1, got %d", strategy.refreshCalls)
	}
	if provider.refreshCalls != 0 {
		t.Fatalf("expected provider refresh not to be called")
	}
}

func TestService_CompleteCallback_MapsStrategyValidationErrors(t *testing.T) {
	ctx := context.Background()

	registry := NewProviderRegistry()
	provider := &strategyProviderStub{
		id:       "custom_api_validation",
		authKind: AuthKindAPIKey,
		strategy: &recordingAuthStrategy{
			kind: AuthKindAPIKey,
			err:  fmt.Errorf("auth: api key token is required"),
		},
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(Config{}, WithRegistry(registry))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID: "custom_api_validation",
		Scope:      ScopeRef{Type: "user", ID: "u1"},
	})
	if err == nil {
		t.Fatalf("expected validation error")
	}

	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.TextCode != ServiceErrorBadInput {
		t.Fatalf("expected %s, got %s", ServiceErrorBadInput, rich.TextCode)
	}
}

func TestService_CompleteCallback_EncryptsNonOAuthCredentialPayload(t *testing.T) {
	ctx := context.Background()

	strategy := &recordingAuthStrategy{
		kind: AuthKindAPIKey,
		completeResponse: AuthCompleteResponse{
			ExternalAccountID: "acct_redaction",
			Credential: ActiveCredential{
				TokenType:   "api_key",
				AccessToken: "plain_secret_token",
			},
		},
	}

	registry := NewProviderRegistry()
	provider := &strategyProviderStub{
		id:       "custom_api_redaction",
		authKind: AuthKindAPIKey,
		strategy: strategy,
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	credentialStore := newMemoryCredentialStore()
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
		WithSecretProvider(testSecretProvider{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	completion, err := svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID: "custom_api_redaction",
		Scope:      ScopeRef{Type: "org", ID: "o1"},
	})
	if err != nil {
		t.Fatalf("complete callback: %v", err)
	}

	stored, err := credentialStore.GetActiveByConnection(ctx, completion.Connection.ID)
	if err != nil {
		t.Fatalf("load stored credential: %v", err)
	}
	if bytes.Contains(stored.EncryptedPayload, []byte("plain_secret_token")) {
		t.Fatalf("expected encrypted payload to avoid plaintext token leakage")
	}
}

func TestService_CompleteCallback_RequiresExternalAccountID(t *testing.T) {
	ctx := context.Background()

	strategy := &recordingAuthStrategy{
		kind: AuthKindAPIKey,
		completeResponse: AuthCompleteResponse{
			Credential: ActiveCredential{
				TokenType:   "api_key",
				AccessToken: "plain_secret_token",
			},
		},
	}

	registry := NewProviderRegistry()
	provider := &strategyProviderStub{
		id:       "custom_api_fallback",
		authKind: AuthKindAPIKey,
		strategy: strategy,
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	credentialStore := newMemoryCredentialStore()
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
		WithSecretProvider(testSecretProvider{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID: "custom_api_fallback",
		Scope:      ScopeRef{Type: "org", ID: "o_fallback"},
	})
	if err == nil {
		t.Fatalf("expected external account id validation error")
	}
}

type strategyProviderStub struct {
	id       string
	authKind AuthKind
	strategy AuthStrategy

	beginCalls    int
	completeCalls int
	refreshCalls  int
}

func (p *strategyProviderStub) ID() string { return p.id }

func (p *strategyProviderStub) AuthKind() AuthKind {
	if p.authKind == "" {
		return AuthKindOAuth2AuthCode
	}
	return p.authKind
}

func (p *strategyProviderStub) SupportedScopeTypes() []string { return []string{"user", "org"} }

func (p *strategyProviderStub) Capabilities() []CapabilityDescriptor {
	return []CapabilityDescriptor{}
}

func (p *strategyProviderStub) BeginAuth(context.Context, BeginAuthRequest) (BeginAuthResponse, error) {
	p.beginCalls++
	return BeginAuthResponse{}, fmt.Errorf("provider begin auth should not be used when AuthStrategy is provided")
}

func (p *strategyProviderStub) CompleteAuth(context.Context, CompleteAuthRequest) (CompleteAuthResponse, error) {
	p.completeCalls++
	return CompleteAuthResponse{}, fmt.Errorf("provider complete auth should not be used when AuthStrategy is provided")
}

func (p *strategyProviderStub) Refresh(context.Context, ActiveCredential) (RefreshResult, error) {
	p.refreshCalls++
	return RefreshResult{}, fmt.Errorf("provider refresh should not be used when AuthStrategy is provided")
}

func (p *strategyProviderStub) AuthStrategy() AuthStrategy { return p.strategy }

type recordingAuthStrategy struct {
	kind AuthKind

	beginResponse    AuthBeginResponse
	completeResponse AuthCompleteResponse
	refreshResponse  RefreshResult
	err              error

	beginCalls    int
	completeCalls int
	refreshCalls  int
}

func (s *recordingAuthStrategy) Type() AuthKind { return s.kind }

func (s *recordingAuthStrategy) Begin(_ context.Context, _ AuthBeginRequest) (AuthBeginResponse, error) {
	s.beginCalls++
	if s.err != nil {
		return AuthBeginResponse{}, s.err
	}
	return s.beginResponse, nil
}

func (s *recordingAuthStrategy) Complete(_ context.Context, _ AuthCompleteRequest) (AuthCompleteResponse, error) {
	s.completeCalls++
	if s.err != nil {
		return AuthCompleteResponse{}, s.err
	}
	return s.completeResponse, nil
}

func (s *recordingAuthStrategy) Refresh(_ context.Context, cred ActiveCredential) (RefreshResult, error) {
	s.refreshCalls++
	if s.err != nil {
		return RefreshResult{}, s.err
	}
	if s.refreshResponse.Credential.TokenType == "" {
		s.refreshResponse.Credential = cred
	}
	return s.refreshResponse, nil
}
