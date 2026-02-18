package core

import (
	"context"
	"errors"
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

func TestConnect_ResolvesCallbackURLWhenRedirectURIMissing(t *testing.T) {
	ctx := context.Background()
	provider := &beginSpyProvider{testProvider: testProvider{id: "github"}}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	stateStore := NewMemoryOAuthStateStore(time.Minute)
	resolver := &recordingCallbackURLResolver{resolvedURL: "https://app.example/callback/resolved"}
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(stateStore),
		WithConnectionStore(newMemoryConnectionStore()),
		WithCredentialStore(newMemoryCredentialStore()),
		WithSecretProvider(testSecretProvider{}),
		WithCallbackURLResolver(resolver),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connectResp, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:      "github",
		Scope:           ScopeRef{Type: "user", ID: "u_resolve"},
		RequestedGrants: []string{"repo:read"},
		Metadata:        map[string]any{"source": "connect"},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if provider.lastBeginAuthRequest.RedirectURI != "https://app.example/callback/resolved" {
		t.Fatalf("expected resolved redirect uri passed to provider begin auth")
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("expected resolver to be called once, got %d", len(resolver.calls))
	}
	if resolver.calls[0].Flow != CallbackURLResolveFlowConnect {
		t.Fatalf("expected connect flow, got %q", resolver.calls[0].Flow)
	}
	if resolver.calls[0].ProviderID != "github" {
		t.Fatalf("expected provider id in resolver request")
	}
	record, err := stateStore.Consume(ctx, connectResp.State)
	if err != nil {
		t.Fatalf("consume oauth state: %v", err)
	}
	if record.RedirectURI != "https://app.example/callback/resolved" {
		t.Fatalf("expected oauth state redirect uri to use resolved value")
	}
}

func TestConnect_DoesNotResolveCallbackURLWhenRedirectURIProvided(t *testing.T) {
	ctx := context.Background()
	provider := &beginSpyProvider{testProvider: testProvider{id: "github"}}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	resolver := &recordingCallbackURLResolver{resolvedURL: "https://app.example/callback/resolved"}
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(NewMemoryOAuthStateStore(time.Minute)),
		WithConnectionStore(newMemoryConnectionStore()),
		WithCredentialStore(newMemoryCredentialStore()),
		WithSecretProvider(testSecretProvider{}),
		WithCallbackURLResolver(resolver),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.Connect(ctx, ConnectRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u_provided"},
		RedirectURI: "https://app.example/callback/provided",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if provider.lastBeginAuthRequest.RedirectURI != "https://app.example/callback/provided" {
		t.Fatalf("expected provided redirect uri passed through")
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("expected resolver not called when redirect uri is provided")
	}
}

func TestConnect_ReturnsErrorWhenCallbackURLResolverFails(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(NewMemoryOAuthStateStore(time.Minute)),
		WithCallbackURLResolver(&recordingCallbackURLResolver{err: errors.New("resolve boom")}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.Connect(ctx, ConnectRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "user", ID: "u_resolver_err"},
	})
	if err == nil || !strings.Contains(err.Error(), "resolve boom") {
		t.Fatalf("expected callback resolver error, got %v", err)
	}
}

func TestStartReconsent_ResolvesCallbackURLWhenRedirectURIMissing(t *testing.T) {
	ctx := context.Background()
	provider := &beginSpyProvider{testProvider: testProvider{id: "github"}}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u_reconsent"},
		ExternalAccountID: "acct_reconsent",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	stateStore := NewMemoryOAuthStateStore(time.Minute)
	resolver := &recordingCallbackURLResolver{resolvedURL: "https://app.example/callback/reconsent"}
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(stateStore),
		WithConnectionStore(connectionStore),
		WithCredentialStore(newMemoryCredentialStore()),
		WithSecretProvider(testSecretProvider{}),
		WithCallbackURLResolver(resolver),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connectResp, err := svc.StartReconsent(ctx, ReconsentRequest{
		ConnectionID:    connection.ID,
		RequestedGrants: []string{"repo:write"},
		Metadata:        map[string]any{"source": "reconsent"},
	})
	if err != nil {
		t.Fatalf("start reconsent: %v", err)
	}
	if provider.lastBeginAuthRequest.RedirectURI != "https://app.example/callback/reconsent" {
		t.Fatalf("expected resolved redirect uri passed to provider begin auth")
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("expected resolver to be called once, got %d", len(resolver.calls))
	}
	call := resolver.calls[0]
	if call.Flow != CallbackURLResolveFlowReconsent {
		t.Fatalf("expected reconsent flow, got %q", call.Flow)
	}
	if call.ConnectionID != connection.ID {
		t.Fatalf("expected connection id in resolver request")
	}
	if got := call.Metadata["connection_id"]; got != connection.ID {
		t.Fatalf("expected connection id in resolver metadata, got %#v", got)
	}

	record, err := stateStore.Consume(ctx, connectResp.State)
	if err != nil {
		t.Fatalf("consume oauth state: %v", err)
	}
	if record.RedirectURI != "https://app.example/callback/reconsent" {
		t.Fatalf("expected oauth state redirect uri to use reconsent resolved value")
	}
	if got := record.Metadata["connection_id"]; got != connection.ID {
		t.Fatalf("expected oauth state metadata to include connection id, got %#v", got)
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

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u1"},
		Code:        "code",
		State:       connectResp.State,
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("expected valid callback to succeed after mismatch, got %v", err)
	}
	if provider.completeCalls != 1 {
		t.Fatalf("expected provider callback to be called once for valid retry, got %d", provider.completeCalls)
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

func TestCompleteCallback_RedirectValidationCannotBeRelaxedByMetadata(t *testing.T) {
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
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "redirect uri is required") {
		t.Fatalf("expected metadata to be unable to relax redirect validation, got %v", err)
	}
}

func TestCompleteCallback_RedirectValidationCanBeHardenedPerRequest(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(
		Config{
			OAuth: OAuthConfig{RequireCallbackRedirect: false},
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
		Scope:       ScopeRef{Type: "user", ID: "u6"},
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, err = svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "user", ID: "u6"},
		Code:       "code",
		State:      connectResp.State,
		Metadata: map[string]any{
			"strict_redirect_validation": true,
		},
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "redirect uri is required") {
		t.Fatalf("expected metadata to harden redirect validation, got %v", err)
	}
}

func TestCompleteCallback_CreatesDistinctConnectionsPerExternalAccount(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	provider := &externalAccountByCodeProvider{
		testProvider: testProvider{id: "github"},
		accountsByCode: map[string]string{
			"code-a": "acct_a",
			"code-b": "acct_b",
		},
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithOAuthStateStore(NewMemoryOAuthStateStore(time.Minute)),
		WithConnectionStore(connectionStore),
		WithCredentialStore(newMemoryCredentialStore()),
		WithSecretProvider(testSecretProvider{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	beginA, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u_multi"},
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("connect A: %v", err)
	}
	completionA, err := svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u_multi"},
		Code:        "code-a",
		State:       beginA.State,
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("complete callback A: %v", err)
	}

	beginB, err := svc.Connect(ctx, ConnectRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u_multi"},
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("connect B: %v", err)
	}
	completionB, err := svc.CompleteCallback(ctx, CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "u_multi"},
		Code:        "code-b",
		State:       beginB.State,
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("complete callback B: %v", err)
	}

	if completionA.Connection.ID == completionB.Connection.ID {
		t.Fatalf("expected separate connections for different external accounts")
	}

	connections, err := connectionStore.FindByScope(
		ctx,
		"github",
		ScopeRef{Type: "user", ID: "u_multi"},
	)
	if err != nil {
		t.Fatalf("find by scope: %v", err)
	}
	if len(connections) != 2 {
		t.Fatalf("expected 2 scoped connections, got %d", len(connections))
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

type externalAccountByCodeProvider struct {
	testProvider
	accountsByCode map[string]string
}

func (p *externalAccountByCodeProvider) CompleteAuth(ctx context.Context, req CompleteAuthRequest) (CompleteAuthResponse, error) {
	response, err := p.testProvider.CompleteAuth(ctx, req)
	if err != nil {
		return CompleteAuthResponse{}, err
	}
	account := strings.TrimSpace(p.accountsByCode[req.Code])
	if account == "" {
		account = "acct_default"
	}
	response.ExternalAccountID = account
	return response, nil
}

type beginSpyProvider struct {
	testProvider
	lastBeginAuthRequest BeginAuthRequest
	beginCalls           int
}

func (p *beginSpyProvider) BeginAuth(ctx context.Context, req BeginAuthRequest) (BeginAuthResponse, error) {
	p.beginCalls++
	p.lastBeginAuthRequest = req
	return p.testProvider.BeginAuth(ctx, req)
}

type recordingCallbackURLResolver struct {
	resolvedURL string
	err         error
	calls       []CallbackURLResolveRequest
}

func (r *recordingCallbackURLResolver) ResolveCallbackURL(_ context.Context, req CallbackURLResolveRequest) (string, error) {
	r.calls = append(r.calls, req)
	if r.err != nil {
		return "", r.err
	}
	return r.resolvedURL, nil
}
