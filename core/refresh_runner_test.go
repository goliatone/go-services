package core

import (
	"context"
	"strings"
	"testing"

	goerrors "github.com/goliatone/go-errors"
)

func TestRunRefreshWithRetry_RetriesAndSucceeds(t *testing.T) {
	ctx := context.Background()

	provider := &scriptedRefreshProvider{
		testProvider: testProvider{id: "github"},
		errs: []error{
			goerrors.New("temporary upstream error", goerrors.CategoryExternal),
			nil,
		},
	}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u1"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	credentialStore := newMemoryCredentialStore()
	if _, err := credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
		ConnectionID:    connection.ID,
		TokenType:       "bearer",
		RequestedScopes: []string{"repo:read"},
		GrantedScopes:   []string{"repo:read"},
		Refreshable:     true,
		Status:          CredentialStatusActive,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
		WithRefreshBackoffScheduler(ExponentialBackoffScheduler{Initial: 0, Max: 0}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.RunRefreshWithRetry(ctx, RefreshRequest{
		ProviderID:   "github",
		ConnectionID: connection.ID,
	}, RefreshRunOptions{MaxAttempts: 3})
	if err != nil {
		t.Fatalf("run refresh with retry: %v", err)
	}
	if result.Attempts != 2 {
		t.Fatalf("expected success on second attempt, got %d", result.Attempts)
	}
}

func TestRunRefreshWithRetry_TransitionsPendingReauthOnUnrecoverableError(t *testing.T) {
	ctx := context.Background()

	provider := &scriptedRefreshProvider{
		testProvider: testProvider{id: "github"},
		errs: []error{
			goerrors.New("invalid refresh token", goerrors.CategoryAuth).WithTextCode("TOKEN_EXPIRED"),
		},
	}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u2"},
		ExternalAccountID: "acct_2",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	credentialStore := newMemoryCredentialStore()
	if _, err := credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
		ConnectionID:    connection.ID,
		TokenType:       "bearer",
		RequestedScopes: []string{"repo:read"},
		GrantedScopes:   []string{"repo:read"},
		Refreshable:     true,
		Status:          CredentialStatusActive,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.RunRefreshWithRetry(ctx, RefreshRequest{
		ProviderID:   "github",
		ConnectionID: connection.ID,
	}, RefreshRunOptions{MaxAttempts: 3})
	if err == nil {
		t.Fatalf("expected unrecoverable refresh error")
	}
	if !result.PendingReauth {
		t.Fatalf("expected pending reauth transition")
	}

	updated, getErr := connectionStore.Get(ctx, connection.ID)
	if getErr != nil {
		t.Fatalf("get updated connection: %v", getErr)
	}
	if updated.Status != ConnectionStatusPendingReauth {
		t.Fatalf("expected pending_reauth status, got %q", updated.Status)
	}
}

func TestRunRefreshWithRetry_FailsWhenLockHeld(t *testing.T) {
	ctx := context.Background()

	locker := NewMemoryConnectionLocker()
	lockHandle, err := locker.Acquire(ctx, "conn_1", defaultRefreshLockTTL)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer func() { _ = lockHandle.Unlock(ctx) }()

	svc, err := NewService(Config{}, WithConnectionLocker(locker))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.RunRefreshWithRetry(ctx, RefreshRequest{
		ProviderID:   "github",
		ConnectionID: "conn_1",
	}, RefreshRunOptions{MaxAttempts: 1})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "lock") {
		t.Fatalf("expected lock contention error, got %v", err)
	}
}

type scriptedRefreshProvider struct {
	testProvider
	errs  []error
	calls int
}

func (p *scriptedRefreshProvider) Refresh(ctx context.Context, cred ActiveCredential) (RefreshResult, error) {
	index := p.calls
	p.calls++
	if index < len(p.errs) && p.errs[index] != nil {
		return RefreshResult{}, p.errs[index]
	}
	return p.testProvider.Refresh(ctx, cred)
}

