package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	goerrors "github.com/goliatone/go-errors"
)

const (
	defaultRefreshMaxAttempts   = 3
	defaultRefreshInitialBackoff = 500 * time.Millisecond
	defaultRefreshMaxBackoff    = 10 * time.Second
	defaultRefreshLockTTL       = 30 * time.Second
)

type LockHandle interface {
	Unlock(ctx context.Context) error
}

type ConnectionLocker interface {
	Acquire(ctx context.Context, connectionID string, ttl time.Duration) (LockHandle, error)
}

type RefreshBackoffScheduler interface {
	NextDelay(attempt int) time.Duration
}

type ExponentialBackoffScheduler struct {
	Initial time.Duration
	Max     time.Duration
}

func (s ExponentialBackoffScheduler) NextDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	initial := s.Initial
	if initial <= 0 {
		initial = defaultRefreshInitialBackoff
	}
	max := s.Max
	if max <= 0 {
		max = defaultRefreshMaxBackoff
	}

	delay := initial
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= max {
			return max
		}
	}
	if delay > max {
		return max
	}
	return delay
}

type RefreshRunResult struct {
	Attempts      int
	PendingReauth bool
}

type RefreshRunOptions struct {
	MaxAttempts int
	LockTTL     time.Duration
}

func (s *Service) RunRefreshWithRetry(ctx context.Context, req RefreshRequest, opts RefreshRunOptions) (RefreshRunResult, error) {
	if s == nil {
		return RefreshRunResult{}, fmt.Errorf("core: service is nil")
	}
	connectionID := strings.TrimSpace(req.ConnectionID)
	if connectionID == "" {
		return RefreshRunResult{}, s.mapError(fmt.Errorf("core: connection id is required"))
	}

	maxAttempts := opts.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = defaultRefreshMaxAttempts
	}
	lockTTL := opts.LockTTL
	if lockTTL <= 0 {
		lockTTL = defaultRefreshLockTTL
	}

	unlock := func() {}
	if s.connectionLocker != nil {
		lockHandle, lockErr := s.connectionLocker.Acquire(ctx, connectionID, lockTTL)
		if lockErr != nil {
			return RefreshRunResult{}, s.mapError(lockErr)
		}
		unlock = func() {
			_ = lockHandle.Unlock(ctx)
		}
	}
	defer unlock()

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := s.Refresh(ctx, req)
		if err == nil {
			return RefreshRunResult{Attempts: attempt}, nil
		}
		lastErr = err

		if isUnrecoverableRefreshError(err) {
			_ = s.transitionConnectionToPendingReauth(ctx, connectionID, err)
			return RefreshRunResult{Attempts: attempt, PendingReauth: true}, s.mapError(err)
		}
		if attempt == maxAttempts {
			_ = s.transitionConnectionToPendingReauth(ctx, connectionID, err)
			return RefreshRunResult{Attempts: attempt, PendingReauth: true}, s.mapError(err)
		}

		delay := defaultRefreshInitialBackoff
		if s.refreshBackoffScheduler != nil {
			delay = s.refreshBackoffScheduler.NextDelay(attempt)
		}
		if waitErr := waitWithContext(ctx, delay); waitErr != nil {
			return RefreshRunResult{Attempts: attempt}, s.mapError(waitErr)
		}
	}

	return RefreshRunResult{Attempts: maxAttempts}, s.mapError(lastErr)
}

func (s *Service) transitionConnectionToPendingReauth(ctx context.Context, connectionID string, source error) error {
	if s == nil || s.connectionStore == nil {
		return nil
	}
	reason := strings.TrimSpace(fmt.Sprint(source))
	if reason == "" {
		reason = "refresh failed"
	}
	return s.connectionStore.UpdateStatus(ctx, connectionID, string(ConnectionStatusPendingReauth), reason)
}

func (s *Service) transitionConnectionToNeedsReconsent(ctx context.Context, connectionID string, reason string) error {
	if s == nil || s.connectionStore == nil {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "re-consent required"
	}
	if err := s.connectionStore.UpdateStatus(ctx, connectionID, string(ConnectionStatusNeedsReconsent), reason); err != nil {
		return err
	}
	if s.grantStore != nil {
		_ = s.grantStore.AppendEvent(ctx, AppendGrantEventInput{
			ConnectionID: connectionID,
			EventType:    GrantEventReconsentRequested,
			OccurredAt:   time.Now().UTC(),
			Metadata: map[string]any{
				"reason": reason,
			},
		})
	}
	return nil
}

func isUnrecoverableRefreshError(err error) bool {
	if err == nil {
		return false
	}
	var richErr *goerrors.Error
	if goerrors.As(err, &richErr) {
		switch richErr.Category {
		case goerrors.CategoryAuth, goerrors.CategoryAuthz, goerrors.CategoryValidation, goerrors.CategoryNotFound:
			return true
		}
		switch strings.TrimSpace(strings.ToUpper(richErr.TextCode)) {
		case "TOKEN_EXPIRED", "UNAUTHORIZED", "FORBIDDEN":
			return true
		}
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "invalid_grant") ||
		strings.Contains(msg, "invalid refresh token") ||
		strings.Contains(msg, "reauthorization required") ||
		strings.Contains(msg, "re-auth required")
}

func waitWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type MemoryConnectionLocker struct {
	mu     sync.Mutex
	locks  map[string]time.Time
	nowFn  func() time.Time
}

func NewMemoryConnectionLocker() *MemoryConnectionLocker {
	return &MemoryConnectionLocker{
		locks: make(map[string]time.Time),
		nowFn: func() time.Time { return time.Now().UTC() },
	}
}

func (l *MemoryConnectionLocker) Acquire(_ context.Context, connectionID string, ttl time.Duration) (LockHandle, error) {
	if l == nil {
		return nil, fmt.Errorf("core: connection locker is not configured")
	}
	connectionID = strings.TrimSpace(connectionID)
	if connectionID == "" {
		return nil, fmt.Errorf("core: connection id is required for lock acquisition")
	}
	if ttl <= 0 {
		ttl = defaultRefreshLockTTL
	}

	now := l.nowFn()
	l.mu.Lock()
	defer l.mu.Unlock()

	if until, ok := l.locks[connectionID]; ok && now.Before(until) {
		return nil, fmt.Errorf("core: refresh lock already held for connection %q", connectionID)
	}
	l.locks[connectionID] = now.Add(ttl)
	return &memoryLockHandle{locker: l, connectionID: connectionID}, nil
}

type memoryLockHandle struct {
	locker       *MemoryConnectionLocker
	connectionID string
	once         sync.Once
}

func (h *memoryLockHandle) Unlock(_ context.Context) error {
	if h == nil || h.locker == nil {
		return nil
	}
	h.once.Do(func() {
		h.locker.mu.Lock()
		delete(h.locker.locks, h.connectionID)
		h.locker.mu.Unlock()
	})
	return nil
}
