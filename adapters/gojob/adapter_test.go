package gojob

import (
	"context"
	"errors"
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
	if err := enqueueAdapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("enqueue: %v", err)
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
		Delay:   30 * time.Second,
		Requeue: true,
		Reason:  "transient",
	}, 1); err != nil {
		t.Fatalf("nack attempt 1: %v", err)
	}
	if rawDelivery.nackOpts.Delay != 10*time.Second {
		t.Fatalf("expected delay to be bounded, got %s", rawDelivery.nackOpts.Delay)
	}
	if !rawDelivery.nackOpts.Requeue {
		t.Fatalf("expected message to be requeued before max attempts")
	}

	if err := adapter.NackForAttempt(ctx, core.JobNackOptions{
		Delay:   time.Second,
		Requeue: true,
		Reason:  "still failing",
	}, 3); err != nil {
		t.Fatalf("nack max attempt: %v", err)
	}
	if rawDelivery.nackOpts.Requeue {
		t.Fatalf("expected no requeue once max attempts is reached")
	}
	if !rawDelivery.nackOpts.DeadLetter {
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
	last *job.ExecutionMessage
}

func (s *stubQueueEnqueuer) Enqueue(_ context.Context, msg *job.ExecutionMessage) error {
	s.last = msg
	return nil
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
