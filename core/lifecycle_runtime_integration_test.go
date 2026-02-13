package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLifecycleRuntime_CompatibilityHooksProjectorsAndActivityControls(t *testing.T) {
	ctx := context.Background()
	outbox := &memoryOutboxStore{}
	coordinator := NewLifecycleHookCoordinator()

	preCalls := 0
	postCalls := 0
	coordinator.RegisterPreCommit(hookFunc{
		name: "pre",
		fn: func(context.Context, LifecycleEvent) error {
			preCalls++
			return nil
		},
	})
	coordinator.RegisterPostCommit(hookFunc{
		name: "post",
		fn: func(context.Context, LifecycleEvent) error {
			postCalls++
			return nil
		},
	})

	primary := &failingPruningSink{deleted: 2}
	fallback := &runtimeCaptureSink{}
	operationalSink, err := NewOperationalActivitySink(primary, fallback, ActivityRetentionPolicy{
		TTL:    24 * time.Hour,
		RowCap: 200,
	}, 8)
	if err != nil {
		t.Fatalf("new operational activity sink: %v", err)
	}
	defer operationalSink.Close()

	activityProjector := NewLifecycleActivityProjector(operationalSink, nil)
	notificationLedger := &memoryDispatchLedger{}
	notificationSender := &capturingSender{}
	notificationProjector := NewGoNotificationsProjector(
		definitionResolverFunc(func(context.Context, LifecycleEvent) (string, bool, error) {
			return "services.connection.failed", true, nil
		}),
		recipientResolverFunc(func(context.Context, LifecycleEvent) ([]Recipient, error) {
			return []Recipient{{Type: "user", ID: "usr_runtime"}}, nil
		}),
		notificationSender,
		notificationLedger,
	)

	registry := NewLifecycleProjectorRegistry()
	registry.Register("activity", activityProjector)
	registry.Register("notifications", notificationProjector)

	dispatcher, err := NewOutboxDispatcher(outbox, registry, OutboxDispatcherConfig{
		BatchSize:      10,
		MaxAttempts:    3,
		InitialBackoff: time.Second,
		MaxBackoff:     8 * time.Second,
	})
	if err != nil {
		t.Fatalf("new outbox dispatcher: %v", err)
	}

	event := LifecycleEvent{
		ID:         "evt_runtime_1",
		Name:       "connection.failed",
		ProviderID: "github",
		ScopeType:  "user",
		ScopeID:    "usr_runtime",
		Source:     "worker",
		OccurredAt: time.Now().UTC(),
		Metadata: map[string]any{
			"status": ServiceActivityStatusWarn,
		},
	}
	if err := coordinator.ExecutePreCommitAndEnqueue(ctx, event, outbox); err != nil {
		t.Fatalf("pre-commit + enqueue: %v", err)
	}

	stats, err := dispatcher.DispatchPending(ctx, 10)
	if err != nil {
		t.Fatalf("dispatch pending: %v", err)
	}
	if err := coordinator.ExecutePostCommit(ctx, event); err != nil {
		t.Fatalf("post-commit hooks: %v", err)
	}

	if preCalls != 1 || postCalls != 1 {
		t.Fatalf("expected one pre and one post hook call, got pre=%d post=%d", preCalls, postCalls)
	}
	if stats.Delivered != 1 || stats.Retried != 0 || stats.Failed != 0 {
		t.Fatalf("unexpected dispatcher stats: %+v", stats)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fallback.count() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fallback.count() != 1 {
		t.Fatalf("expected activity fallback sink write")
	}
	if len(notificationSender.requests) != 1 {
		t.Fatalf("expected notification send")
	}
	if len(notificationLedger.records) != 1 {
		t.Fatalf("expected notification ledger record")
	}

	deleted, err := operationalSink.EnforceRetention(ctx)
	if err != nil {
		t.Fatalf("enforce retention: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected retention delegation count=2, got %d", deleted)
	}
}

type memoryOutboxStore struct {
	events []LifecycleEvent
}

func (s *memoryOutboxStore) Enqueue(_ context.Context, event LifecycleEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *memoryOutboxStore) ClaimBatch(_ context.Context, limit int) ([]LifecycleEvent, error) {
	if limit <= 0 || len(s.events) == 0 {
		return nil, nil
	}
	if limit > len(s.events) {
		limit = len(s.events)
	}
	out := append([]LifecycleEvent(nil), s.events[:limit]...)
	s.events = append([]LifecycleEvent(nil), s.events[limit:]...)
	return out, nil
}

func (s *memoryOutboxStore) Ack(context.Context, string) error { return nil }

func (s *memoryOutboxStore) Retry(_ context.Context, _ string, _ error, nextAttemptAt time.Time) error {
	if nextAttemptAt.IsZero() {
		return nil
	}
	return errors.New("unexpected retry in runtime integration test")
}

type failingPruningSink struct {
	deleted int
}

func (s *failingPruningSink) Record(context.Context, ServiceActivityEntry) error {
	return errors.New("primary activity sink failure")
}

func (s *failingPruningSink) List(context.Context, ServicesActivityFilter) (ServicesActivityPage, error) {
	return ServicesActivityPage{}, nil
}

func (s *failingPruningSink) Prune(context.Context, ActivityRetentionPolicy) (int, error) {
	return s.deleted, nil
}

type runtimeCaptureSink struct {
	mu      sync.Mutex
	entries []ServiceActivityEntry
}

func (s *runtimeCaptureSink) Record(_ context.Context, entry ServiceActivityEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	return nil
}

func (s *runtimeCaptureSink) List(context.Context, ServicesActivityFilter) (ServicesActivityPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := append([]ServiceActivityEntry(nil), s.entries...)
	return ServicesActivityPage{Items: items, Total: len(items)}, nil
}

func (s *runtimeCaptureSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}
