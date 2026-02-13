package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLifecycleProjectorRegistry_DeterministicOrder(t *testing.T) {
	registry := NewLifecycleProjectorRegistry()
	calls := make([]string, 0, 3)
	registry.Register("zeta", lifecycleHandlerFunc(func(context.Context, LifecycleEvent) error {
		calls = append(calls, "zeta")
		return nil
	}))
	registry.Register("alpha", lifecycleHandlerFunc(func(context.Context, LifecycleEvent) error {
		calls = append(calls, "alpha")
		return nil
	}))
	registry.Register("m", lifecycleHandlerFunc(func(context.Context, LifecycleEvent) error {
		calls = append(calls, "m")
		return nil
	}))

	handlers := registry.Handlers()
	if len(handlers) != 3 {
		t.Fatalf("expected 3 handlers, got %d", len(handlers))
	}
	for _, handler := range handlers {
		if err := handler.Handle(context.Background(), LifecycleEvent{ID: "evt_1"}); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}
	expected := []string{"alpha", "m", "zeta"}
	for i := range expected {
		if calls[i] != expected[i] {
			t.Fatalf("expected call order %v, got %v", expected, calls)
		}
	}
}

func TestLifecycleActivityProjector_MapsEventToActivityEntry(t *testing.T) {
	sink := &capturingActivitySink{}
	projector := NewLifecycleActivityProjector(sink, nil)

	event := LifecycleEvent{
		ID:           "evt_1",
		Name:         "connection.connected",
		ProviderID:   "github",
		ScopeType:    "user",
		ScopeID:      "usr_1",
		ConnectionID: "conn_1",
		Source:       "api",
		OccurredAt:   time.Now().UTC(),
		Payload:      map[string]any{"status": "ok"},
	}
	if err := projector.Handle(context.Background(), event); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if sink.last.Action != "connection.connected" {
		t.Fatalf("expected action mapping")
	}
	if sink.last.Actor != "api" {
		t.Fatalf("expected actor mapping")
	}
	if sink.last.Channel != DefaultLifecycleChannel {
		t.Fatalf("expected lifecycle channel")
	}
	if sink.last.Object != "connection:conn_1" {
		t.Fatalf("expected connection object mapping")
	}
}

func TestGoNotificationsProjector_IdempotentDispatch(t *testing.T) {
	ledger := &memoryDispatchLedger{}
	sender := &capturingSender{}
	projector := NewGoNotificationsProjector(
		definitionResolverFunc(func(context.Context, LifecycleEvent) (string, bool, error) {
			return "services.connection.failed", true, nil
		}),
		recipientResolverFunc(func(context.Context, LifecycleEvent) ([]Recipient, error) {
			return []Recipient{
				{Type: "user", ID: "usr_1"},
				{Type: "user", ID: "usr_2"},
			}, nil
		}),
		sender,
		ledger,
	)

	event := LifecycleEvent{
		ID:         "evt_1",
		Name:       "connection.failed",
		ProviderID: "github",
		ScopeType:  "user",
		ScopeID:    "usr_1",
		OccurredAt: time.Now().UTC(),
	}
	if err := projector.Handle(context.Background(), event); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if len(sender.requests) != 2 {
		t.Fatalf("expected two sends on first dispatch, got %d", len(sender.requests))
	}

	if err := projector.Handle(context.Background(), event); err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if len(sender.requests) != 2 {
		t.Fatalf("expected idempotent no-op on second dispatch, got %d sends", len(sender.requests))
	}
	if len(ledger.records) != 2 {
		t.Fatalf("expected two dispatch ledger records")
	}
}

func TestGoNotificationsProjector_RecordsFailure(t *testing.T) {
	ledger := &memoryDispatchLedger{}
	sender := &capturingSender{err: errors.New("delivery failure")}
	projector := NewGoNotificationsProjector(
		definitionResolverFunc(func(context.Context, LifecycleEvent) (string, bool, error) {
			return "services.sync.failed", true, nil
		}),
		recipientResolverFunc(func(context.Context, LifecycleEvent) ([]Recipient, error) {
			return []Recipient{{Type: "org", ID: "org_1"}}, nil
		}),
		sender,
		ledger,
	)

	err := projector.Handle(context.Background(), LifecycleEvent{
		ID:         "evt_fail",
		Name:       "sync.failed",
		ProviderID: "github",
		ScopeType:  "org",
		ScopeID:    "org_1",
		OccurredAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatalf("expected send failure")
	}
	if len(ledger.records) != 1 {
		t.Fatalf("expected failed dispatch ledger record")
	}
	if ledger.records[0].Status != "failed" {
		t.Fatalf("expected failed dispatch status")
	}
}

type lifecycleHandlerFunc func(context.Context, LifecycleEvent) error

func (fn lifecycleHandlerFunc) Handle(ctx context.Context, event LifecycleEvent) error {
	return fn(ctx, event)
}

type capturingActivitySink struct {
	last ServiceActivityEntry
}

func (s *capturingActivitySink) Record(_ context.Context, entry ServiceActivityEntry) error {
	s.last = entry
	return nil
}

func (s *capturingActivitySink) List(context.Context, ServicesActivityFilter) (ServicesActivityPage, error) {
	return ServicesActivityPage{}, nil
}

type definitionResolverFunc func(context.Context, LifecycleEvent) (string, bool, error)

func (fn definitionResolverFunc) Resolve(ctx context.Context, event LifecycleEvent) (string, bool, error) {
	return fn(ctx, event)
}

type recipientResolverFunc func(context.Context, LifecycleEvent) ([]Recipient, error)

func (fn recipientResolverFunc) Resolve(ctx context.Context, event LifecycleEvent) ([]Recipient, error) {
	return fn(ctx, event)
}

type capturingSender struct {
	requests []NotificationSendRequest
	err      error
}

func (s *capturingSender) Send(_ context.Context, req NotificationSendRequest) error {
	s.requests = append(s.requests, req)
	return s.err
}

type memoryDispatchLedger struct {
	records []NotificationDispatchRecord
	seen    map[string]struct{}
}

func (l *memoryDispatchLedger) Seen(_ context.Context, idempotencyKey string) (bool, error) {
	if l.seen == nil {
		l.seen = make(map[string]struct{})
	}
	_, ok := l.seen[idempotencyKey]
	return ok, nil
}

func (l *memoryDispatchLedger) Record(_ context.Context, record NotificationDispatchRecord) error {
	if l.seen == nil {
		l.seen = make(map[string]struct{})
	}
	l.records = append(l.records, record)
	l.seen[record.IdempotencyKey] = struct{}{}
	return nil
}
