package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestRefresh_IdempotentCredentialRotationUnderConcurrentExecution(t *testing.T) {
	ctx := context.Background()
	expiresInitial := time.Now().UTC().Add(30 * time.Minute)
	expiresRefreshed := time.Now().UTC().Add(2 * time.Hour)

	provider := &stableRefreshProvider{
		testProvider: testProvider{id: "github"},
		result: RefreshResult{
			Credential: ActiveCredential{
				TokenType:       "bearer",
				RequestedScopes: []string{"repo:read"},
				GrantedScopes:   []string{"repo:read"},
				Refreshable:     true,
				ExpiresAt:       &expiresRefreshed,
			},
		},
		delay: 50 * time.Millisecond,
	}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u3"},
		ExternalAccountID: "acct_3",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	credentialStore := newMemoryCredentialStore()
	if _, err := credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
		ConnectionID:     connection.ID,
		EncryptedPayload: []byte("token-1"),
		TokenType:        "bearer",
		RequestedScopes:  []string{"repo:read"},
		GrantedScopes:    []string{"repo:read"},
		ExpiresAt:        &expiresInitial,
		Refreshable:      true,
		Status:           CredentialStatusActive,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
		WithConnectionLocker(NewMemoryConnectionLocker()),
		WithRefreshBackoffScheduler(ExponentialBackoffScheduler{Initial: 0, Max: 0}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, refreshErr := svc.Refresh(ctx, RefreshRequest{
				ProviderID:   "github",
				ConnectionID: connection.ID,
			})
			errCh <- refreshErr
		}()
	}
	wg.Wait()
	close(errCh)
	successCount := 0
	lockErrorCount := 0
	for refreshErr := range errCh {
		if refreshErr == nil {
			successCount++
			continue
		}
		if strings.Contains(strings.ToLower(refreshErr.Error()), "service_refresh_locked") {
			lockErrorCount++
			continue
		}
		t.Fatalf("refresh failed: %v", refreshErr)
	}
	if successCount != 1 || lockErrorCount != 1 {
		t.Fatalf("expected one success and one lock conflict, got success=%d lock=%d", successCount, lockErrorCount)
	}

	active, err := credentialStore.GetActiveByConnection(ctx, connection.ID)
	if err != nil {
		t.Fatalf("get active credential: %v", err)
	}
	if active.Version != 2 {
		t.Fatalf("expected exactly one rotated version under concurrent refresh, got version=%d", active.Version)
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

type stableRefreshProvider struct {
	testProvider
	result RefreshResult
	delay  time.Duration
}

func (p *stableRefreshProvider) Refresh(_ context.Context, _ ActiveCredential) (RefreshResult, error) {
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	return p.result, nil
}
