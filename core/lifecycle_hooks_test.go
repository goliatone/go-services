package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLifecycleHookCoordinator_PreCommitFailFast(t *testing.T) {
	coordinator := NewLifecycleHookCoordinator()
	calls := make([]string, 0, 3)

	coordinator.RegisterPreCommit(hookFunc{
		name: "first",
		fn: func(context.Context, LifecycleEvent) error {
			calls = append(calls, "first")
			return nil
		},
	})
	coordinator.RegisterPreCommit(hookFunc{
		name: "second",
		fn: func(context.Context, LifecycleEvent) error {
			calls = append(calls, "second")
			return errors.New("fail")
		},
	})
	coordinator.RegisterPreCommit(hookFunc{
		name: "third",
		fn: func(context.Context, LifecycleEvent) error {
			calls = append(calls, "third")
			return nil
		},
	})

	err := coordinator.ExecutePreCommit(context.Background(), LifecycleEvent{ID: "evt_1"})
	if err == nil {
		t.Fatalf("expected pre-commit failure")
	}
	if len(calls) != 2 {
		t.Fatalf("expected fail-fast behavior with 2 calls, got %d", len(calls))
	}
}

func TestLifecycleHookCoordinator_PostCommitAggregatesErrors(t *testing.T) {
	coordinator := NewLifecycleHookCoordinator()
	calls := 0

	coordinator.RegisterPostCommit(hookFunc{
		name: "one",
		fn: func(context.Context, LifecycleEvent) error {
			calls++
			return errors.New("boom-1")
		},
	})
	coordinator.RegisterPostCommit(hookFunc{
		name: "two",
		fn: func(context.Context, LifecycleEvent) error {
			calls++
			return nil
		},
	})
	coordinator.RegisterPostCommit(hookFunc{
		name: "three",
		fn: func(context.Context, LifecycleEvent) error {
			calls++
			return errors.New("boom-3")
		},
	})

	err := coordinator.ExecutePostCommit(context.Background(), LifecycleEvent{ID: "evt_2"})
	if err == nil {
		t.Fatalf("expected aggregated post-commit error")
	}
	if calls != 3 {
		t.Fatalf("expected all post-commit hooks to execute, got %d", calls)
	}
}

func TestLifecycleHookCoordinator_PreCommitAndEnqueueGuarantee(t *testing.T) {
	coordinator := NewLifecycleHookCoordinator()
	store := &hookOutboxStore{}
	event := LifecycleEvent{ID: "evt_3", Name: "connection.connected", ProviderID: "github", ScopeType: "user", ScopeID: "usr_1"}

	coordinator.RegisterPreCommit(hookFunc{
		name: "fails",
		fn: func(context.Context, LifecycleEvent) error {
			return errors.New("precommit rejected")
		},
	})
	if err := coordinator.ExecutePreCommitAndEnqueue(context.Background(), event, store); err == nil {
		t.Fatalf("expected failure when pre-commit hook fails")
	}
	if store.enqueued != 0 {
		t.Fatalf("expected no enqueue when pre-commit fails")
	}

	coordinator = NewLifecycleHookCoordinator()
	coordinator.RegisterPreCommit(hookFunc{name: "ok", fn: func(context.Context, LifecycleEvent) error { return nil }})
	if err := coordinator.ExecutePreCommitAndEnqueue(context.Background(), event, store); err != nil {
		t.Fatalf("expected enqueue on successful pre-commit: %v", err)
	}
	if store.enqueued != 1 {
		t.Fatalf("expected one enqueue, got %d", store.enqueued)
	}
}

type hookFunc struct {
	name string
	fn   func(context.Context, LifecycleEvent) error
}

func (h hookFunc) Name() string { return h.name }

func (h hookFunc) OnEvent(ctx context.Context, event LifecycleEvent) error {
	if h.fn == nil {
		return nil
	}
	return h.fn(ctx, event)
}

type hookOutboxStore struct {
	enqueued int
}

func (s *hookOutboxStore) Enqueue(context.Context, LifecycleEvent) error {
	s.enqueued++
	return nil
}

func (s *hookOutboxStore) ClaimBatch(context.Context, int) ([]LifecycleEvent, error) {
	return nil, nil
}

func (s *hookOutboxStore) Ack(context.Context, string) error {
	return nil
}

func (s *hookOutboxStore) Retry(context.Context, string, error, time.Time) error {
	return nil
}
