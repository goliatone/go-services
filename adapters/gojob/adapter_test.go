package gojob

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"

	job "github.com/goliatone/go-job"
	"github.com/goliatone/go-job/queue"
	"github.com/goliatone/go-job/queue/worker"
)

func TestMessageMappingRoundTrip(t *testing.T) {
	original := &core.JobExecutionMessage{
		JobID:          JobIDRefresh,
		ScriptPath:     "services.refresh",
		Parameters:     map[string]any{"connection_id": "conn_1"},
		IdempotencyKey: "idem-1",
		DedupPolicy:    "drop",
	}

	converted := ToExecutionMessage(original)
	if converted == nil {
		t.Fatalf("expected converted message")
	}
	roundTrip := FromExecutionMessage(converted)
	if roundTrip.JobID != original.JobID {
		t.Fatalf("expected job id %q, got %q", original.JobID, roundTrip.JobID)
	}
	if roundTrip.ScriptPath != original.ScriptPath {
		t.Fatalf("expected script path %q, got %q", original.ScriptPath, roundTrip.ScriptPath)
	}
	if roundTrip.IdempotencyKey != original.IdempotencyKey {
		t.Fatalf("expected idempotency key %q, got %q", original.IdempotencyKey, roundTrip.IdempotencyKey)
	}
	if roundTrip.DedupPolicy != original.DedupPolicy {
		t.Fatalf("expected dedup policy %q, got %q", original.DedupPolicy, roundTrip.DedupPolicy)
	}
	if roundTrip.Parameters["connection_id"] != "conn_1" {
		t.Fatalf("expected parameters to survive mapping")
	}
}

func TestEnqueueAndDequeueAdapters(t *testing.T) {
	ctx := context.Background()
	enqueuer := &stubQueueEnqueuer{}
	enqueueAdapter := NewEnqueuerAdapter(enqueuer)

	msg := &core.JobExecutionMessage{
		JobID:          JobIDOutboxDispatch,
		ScriptPath:     "services.outbox.dispatch",
		Parameters:     map[string]any{"batch_size": 50},
		IdempotencyKey: "idem-outbox",
		DedupPolicy:    "merge",
	}
	receipt, err := enqueueAdapter.Enqueue(ctx, msg)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if receipt.DispatchID == "" || receipt.EnqueuedAt.IsZero() {
		t.Fatalf("expected enqueue receipt metadata, got %+v", receipt)
	}
	if enqueuer.last == nil || enqueuer.last.JobID != JobIDOutboxDispatch {
		t.Fatalf("expected mapped go-job message")
	}

	dequeuer := &stubQueueDequeuer{delivery: &stubQueueDelivery{msg: enqueuer.last}}
	dequeueAdapter := NewDequeuerAdapter(dequeuer, RetryPolicy{})
	delivery, err := dequeueAdapter.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	got := delivery.Message()
	if got == nil || got.JobID != JobIDOutboxDispatch {
		t.Fatalf("expected mapped core message")
	}
	if err := delivery.Ack(ctx); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if !dequeuer.delivery.(*stubQueueDelivery).acked {
		t.Fatalf("expected ack on underlying delivery")
	}
}

func TestEnqueueAdapterScheduledAndDispatchStatus(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	next := now.Add(2 * time.Minute)
	enqueuer := &stubQueueEnqueuer{
		status: queue.DispatchStatus{
			DispatchID:     "dispatch-after",
			State:          queue.DispatchStateRetrying,
			Attempt:        3,
			EnqueuedAt:     &now,
			UpdatedAt:      &now,
			NextRunAt:      &next,
			TerminalReason: "transient",
		},
	}
	adapter := NewEnqueuerAdapter(enqueuer)
	msg := &core.JobExecutionMessage{
		JobID:      JobIDRefresh,
		ScriptPath: "services.refresh",
	}

	runAt := now.Add(time.Minute)
	receipt, err := adapter.EnqueueAt(ctx, msg, runAt)
	if err != nil {
		t.Fatalf("enqueue at: %v", err)
	}
	if receipt.DispatchID != "dispatch-at" {
		t.Fatalf("expected enqueue-at receipt, got %+v", receipt)
	}
	if !enqueuer.lastAt.Equal(runAt) {
		t.Fatalf("expected enqueue-at schedule to be forwarded")
	}

	receipt, err = adapter.EnqueueAfter(ctx, msg, 30*time.Second)
	if err != nil {
		t.Fatalf("enqueue after: %v", err)
	}
	if receipt.DispatchID != "dispatch-after" {
		t.Fatalf("expected enqueue-after receipt, got %+v", receipt)
	}
	if enqueuer.lastDelay != 30*time.Second {
		t.Fatalf("expected enqueue-after delay to be forwarded")
	}

	status, err := adapter.GetDispatchStatus(ctx, "dispatch-after")
	if err != nil {
		t.Fatalf("dispatch status: %v", err)
	}
	if status.DispatchID != "dispatch-after" || status.State != core.JobDispatchStateRetrying {
		t.Fatalf("expected mapped dispatch status, got %+v", status)
	}
	if status.EnqueuedAt == nil || status.EnqueuedAt.IsZero() {
		t.Fatalf("expected enqueued_at status timestamp")
	}
	if status.NextRunAt == nil || status.NextRunAt.IsZero() {
		t.Fatalf("expected next_run_at status timestamp")
	}
}

func TestEnqueueAdapterUnsupportedScheduledAndStatus(t *testing.T) {
	ctx := context.Background()
	adapter := NewEnqueuerAdapter(&stubBasicQueueEnqueuer{})
	msg := &core.JobExecutionMessage{
		JobID:      JobIDRefresh,
		ScriptPath: "services.refresh",
	}

	if _, err := adapter.EnqueueAt(ctx, msg, time.Now().UTC().Add(time.Minute)); !errors.Is(err, queue.ErrScheduledEnqueueUnsupported) {
		t.Fatalf("expected scheduled enqueue unsupported, got %v", err)
	}
	if _, err := adapter.EnqueueAfter(ctx, msg, time.Second); !errors.Is(err, queue.ErrScheduledEnqueueUnsupported) {
		t.Fatalf("expected delayed enqueue unsupported, got %v", err)
	}
	if _, err := adapter.GetDispatchStatus(ctx, "dispatch-missing"); !errors.Is(err, queue.ErrDispatchStatusUnsupported) {
		t.Fatalf("expected dispatch status unsupported, got %v", err)
	}
}

func TestNackRetryPolicyBoundaries(t *testing.T) {
	ctx := context.Background()
	rawDelivery := &stubQueueDelivery{
		msg: &job.ExecutionMessage{
			JobID:      JobIDSyncIncremental,
			ScriptPath: "services.sync.incremental",
		},
	}
	adapter := NewDeliveryAdapter(rawDelivery, RetryPolicy{
		MaxAttempts:     3,
		MaxDelay:        10 * time.Second,
		DeadLetterOnMax: true,
	})

	if err := adapter.NackForAttempt(ctx, core.JobNackOptions{
		Disposition: core.JobNackDispositionRetry,
		Delay:       30 * time.Second,
		Reason:      "transient",
	}, 1); err != nil {
		t.Fatalf("nack attempt 1: %v", err)
	}
	if rawDelivery.nackOpts.Delay != 10*time.Second {
		t.Fatalf("expected delay to be bounded, got %s", rawDelivery.nackOpts.Delay)
	}
	if rawDelivery.nackOpts.Disposition != queue.NackDispositionRetry {
		t.Fatalf("expected message to be requeued before max attempts")
	}

	if err := adapter.NackForAttempt(ctx, core.JobNackOptions{
		Disposition: core.JobNackDispositionRetry,
		Delay:       time.Second,
		Reason:      "still failing",
	}, 3); err != nil {
		t.Fatalf("nack max attempt: %v", err)
	}
	if rawDelivery.nackOpts.Disposition != queue.NackDispositionDeadLetter {
		t.Fatalf("expected dead letter on max attempts")
	}
}

func TestWorkerHookAdapterEventMapping(t *testing.T) {
	now := time.Now().UTC().Add(-time.Second)
	coreHook := &capturingHook{}
	adapter := NewWorkerHookAdapter(coreHook)

	evt := worker.Event{
		Message: &job.ExecutionMessage{
			JobID:          JobIDSubscriptionRenew,
			ScriptPath:     "services.subscription.renew",
			IdempotencyKey: "idem-sub",
		},
		Attempt:   2,
		Delay:     5 * time.Second,
		Err:       errors.New("retry"),
		StartedAt: now,
		Duration:  250 * time.Millisecond,
	}

	adapter.OnRetry(context.Background(), evt)
	if coreHook.last.Message == nil {
		t.Fatalf("expected worker message mapping")
	}
	if coreHook.last.Message.JobID != JobIDSubscriptionRenew {
		t.Fatalf("expected job id mapping, got %q", coreHook.last.Message.JobID)
	}
	if coreHook.last.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", coreHook.last.Attempt)
	}
	if coreHook.last.Delay != 5*time.Second {
		t.Fatalf("expected delay 5s, got %s", coreHook.last.Delay)
	}
	if coreHook.last.Duration != 250*time.Millisecond {
		t.Fatalf("expected duration mapping")
	}
	if coreHook.last.StartedAt.IsZero() {
		t.Fatalf("expected started_at mapping")
	}
	if coreHook.last.Err == nil || coreHook.last.Err.Error() != "retry" {
		t.Fatalf("expected error mapping")
	}
}

type stubQueueEnqueuer struct {
	last      *job.ExecutionMessage
	lastAt    time.Time
	lastDelay time.Duration
	status    queue.DispatchStatus
}

func (s *stubQueueEnqueuer) Enqueue(_ context.Context, msg *job.ExecutionMessage) (queue.EnqueueReceipt, error) {
	s.last = msg
	return queue.EnqueueReceipt{
		DispatchID: "dispatch-test",
		EnqueuedAt: time.Now().UTC(),
	}, nil
}

func (s *stubQueueEnqueuer) EnqueueAt(_ context.Context, msg *job.ExecutionMessage, at time.Time) (queue.EnqueueReceipt, error) {
	s.last = msg
	s.lastAt = at
	return queue.EnqueueReceipt{
		DispatchID: "dispatch-at",
		EnqueuedAt: time.Now().UTC(),
	}, nil
}

func (s *stubQueueEnqueuer) EnqueueAfter(_ context.Context, msg *job.ExecutionMessage, delay time.Duration) (queue.EnqueueReceipt, error) {
	s.last = msg
	s.lastDelay = delay
	return queue.EnqueueReceipt{
		DispatchID: "dispatch-after",
		EnqueuedAt: time.Now().UTC(),
	}, nil
}

func (s *stubQueueEnqueuer) GetDispatchStatus(_ context.Context, dispatchID string) (queue.DispatchStatus, error) {
	if strings.TrimSpace(dispatchID) == "" || strings.TrimSpace(s.status.DispatchID) != strings.TrimSpace(dispatchID) {
		return queue.DispatchStatus{}, queue.ErrDispatchNotFound
	}
	return s.status, nil
}

type stubBasicQueueEnqueuer struct{}

func (stubBasicQueueEnqueuer) Enqueue(_ context.Context, _ *job.ExecutionMessage) (queue.EnqueueReceipt, error) {
	return queue.EnqueueReceipt{
		DispatchID: "dispatch-basic",
		EnqueuedAt: time.Now().UTC(),
	}, nil
}

type stubQueueDequeuer struct {
	delivery queue.Delivery
}

func (s *stubQueueDequeuer) Dequeue(context.Context) (queue.Delivery, error) {
	return s.delivery, nil
}

type stubQueueDelivery struct {
	msg      *job.ExecutionMessage
	acked    bool
	nackOpts queue.NackOptions
}

func (s *stubQueueDelivery) Message() *job.ExecutionMessage {
	return s.msg
}

func (s *stubQueueDelivery) Ack(context.Context) error {
	s.acked = true
	return nil
}

func (s *stubQueueDelivery) Nack(_ context.Context, opts queue.NackOptions) error {
	s.nackOpts = opts
	return nil
}

type capturingHook struct {
	last core.JobWorkerEvent
}

func (h *capturingHook) OnStart(context.Context, core.JobWorkerEvent)   {}
func (h *capturingHook) OnSuccess(context.Context, core.JobWorkerEvent) {}
func (h *capturingHook) OnFailure(context.Context, core.JobWorkerEvent) {}
func (h *capturingHook) OnRetry(_ context.Context, event core.JobWorkerEvent) {
	h.last = event
}
