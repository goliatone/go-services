package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOutboxDispatcher_AckSuccess(t *testing.T) {
	store := &stubOutboxStore{
		claimed: []LifecycleEvent{{
			ID:   "evt_1",
			Name: "connection.connected",
		}},
	}
	registry := &stubProjectorRegistry{}
	registry.Register("ok", lifecycleEventHandlerFunc(func(context.Context, LifecycleEvent) error {
		return nil
	}))

	dispatcher, err := NewOutboxDispatcher(store, registry, DefaultOutboxDispatcherConfig())
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	stats, err := dispatcher.DispatchPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("dispatch pending: %v", err)
	}
	if stats.Claimed != 1 || stats.Delivered != 1 || stats.Retried != 0 || stats.Failed != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if len(store.acked) != 1 || store.acked[0] != "evt_1" {
		t.Fatalf("expected ack for evt_1")
	}
}

func TestOutboxDispatcher_RetryWithBackoff(t *testing.T) {
	store := &stubOutboxStore{
		claimed: []LifecycleEvent{{
			ID:   "evt_retry",
			Name: "connection.refresh_failed",
			Metadata: map[string]any{
				MetadataKeyOutboxAttempts: 1,
			},
		}},
	}
	registry := &stubProjectorRegistry{}
	registry.Register("fails", lifecycleEventHandlerFunc(func(context.Context, LifecycleEvent) error {
		return errors.New("temporary")
	}))

	dispatcher, err := NewOutboxDispatcher(store, registry, OutboxDispatcherConfig{
		BatchSize:      10,
		MaxAttempts:    4,
		InitialBackoff: time.Second,
		MaxBackoff:     8 * time.Second,
	})
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	stats, err := dispatcher.DispatchPending(context.Background(), 0)
	if err == nil {
		t.Fatalf("expected dispatch error")
	}
	if stats.Retried != 1 || stats.Failed != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if len(store.retried) != 1 {
		t.Fatalf("expected one retry call")
	}
	if store.retried[0].next.IsZero() {
		t.Fatalf("expected non-zero next_attempt_at for retry")
	}
}

func TestOutboxDispatcher_MaxAttemptsMarkedFailed(t *testing.T) {
	store := &stubOutboxStore{
		claimed: []LifecycleEvent{{
			ID:   "evt_fail",
			Name: "sync.failed",
			Metadata: map[string]any{
				MetadataKeyOutboxAttempts: 2,
			},
		}},
	}
	registry := &stubProjectorRegistry{}
	registry.Register("fails", lifecycleEventHandlerFunc(func(context.Context, LifecycleEvent) error {
		return errors.New("permanent")
	}))

	dispatcher, err := NewOutboxDispatcher(store, registry, OutboxDispatcherConfig{
		BatchSize:      10,
		MaxAttempts:    3,
		InitialBackoff: time.Second,
		MaxBackoff:     8 * time.Second,
	})
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	stats, err := dispatcher.DispatchPending(context.Background(), 10)
	if err == nil {
		t.Fatalf("expected dispatch error")
	}
	if stats.Failed != 1 || stats.Retried != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if len(store.retried) != 1 {
		t.Fatalf("expected one retry/fail call")
	}
	if !store.retried[0].next.IsZero() {
		t.Fatalf("expected zero next attempt to mark failed")
	}
}

type stubOutboxStore struct {
	claimed []LifecycleEvent
	acked   []string
	retried []retryCall
}

type retryCall struct {
	eventID string
	cause   error
	next    time.Time
}

func (s *stubOutboxStore) Enqueue(context.Context, LifecycleEvent) error {
	return nil
}

func (s *stubOutboxStore) ClaimBatch(context.Context, int) ([]LifecycleEvent, error) {
	out := append([]LifecycleEvent(nil), s.claimed...)
	s.claimed = nil
	return out, nil
}

func (s *stubOutboxStore) Ack(_ context.Context, eventID string) error {
	s.acked = append(s.acked, eventID)
	return nil
}

func (s *stubOutboxStore) Retry(_ context.Context, eventID string, cause error, nextAttemptAt time.Time) error {
	s.retried = append(s.retried, retryCall{
		eventID: eventID,
		cause:   cause,
		next:    nextAttemptAt,
	})
	return nil
}

type lifecycleEventHandlerFunc func(ctx context.Context, event LifecycleEvent) error

func (fn lifecycleEventHandlerFunc) Handle(ctx context.Context, event LifecycleEvent) error {
	return fn(ctx, event)
}

type stubProjectorRegistry struct {
	handlers []LifecycleEventHandler
}

func (s *stubProjectorRegistry) Register(_ string, handler LifecycleEventHandler) {
	if handler == nil {
		return
	}
	s.handlers = append(s.handlers, handler)
}

func (s *stubProjectorRegistry) Handlers() []LifecycleEventHandler {
	return append([]LifecycleEventHandler(nil), s.handlers...)
}
