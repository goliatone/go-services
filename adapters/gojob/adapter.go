package gojob

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"

	job "github.com/goliatone/go-job"
	"github.com/goliatone/go-job/queue"
	"github.com/goliatone/go-job/queue/worker"
)

const (
	JobIDRefresh           = "services.refresh"
	JobIDSyncIncremental   = "services.sync.incremental"
	JobIDSubscriptionRenew = "services.subscription.renew"
	JobIDOutboxDispatch    = "services.outbox.dispatch"
)

// RetryPolicy defines queue retry bounds to avoid unbounded retry loops.
type RetryPolicy struct {
	MaxAttempts     int
	MaxDelay        time.Duration
	DeadLetterOnMax bool
}

// NormalizeAttempt enforces bounded retry behavior for a nack operation.
func (p RetryPolicy) NormalizeAttempt(opts core.JobNackOptions, attempt int) core.JobNackOptions {
	out := opts
	out.Reason = strings.TrimSpace(out.Reason)
	if out.Delay < 0 {
		out.Delay = 0
	}
	if p.MaxDelay > 0 && out.Delay > p.MaxDelay {
		out.Delay = p.MaxDelay
	}
	if out.DeadLetter {
		out.Requeue = false
	}
	if p.MaxAttempts > 0 && attempt >= p.MaxAttempts {
		out.Requeue = false
		if p.DeadLetterOnMax || out.DeadLetter {
			out.DeadLetter = true
		}
	}
	if !out.Requeue && !out.DeadLetter {
		out.Requeue = true
	}
	return out
}

// ToExecutionMessage maps a go-services runtime message to go-job.
func ToExecutionMessage(msg *core.JobExecutionMessage) *job.ExecutionMessage {
	if msg == nil {
		return nil
	}
	return &job.ExecutionMessage{
		JobID:          strings.TrimSpace(msg.JobID),
		ScriptPath:     strings.TrimSpace(msg.ScriptPath),
		Parameters:     copyAnyMap(msg.Parameters),
		IdempotencyKey: strings.TrimSpace(msg.IdempotencyKey),
		DedupPolicy:    job.DeduplicationPolicy(strings.TrimSpace(msg.DedupPolicy)),
	}
}

// FromExecutionMessage maps a go-job message into the go-services contract.
func FromExecutionMessage(msg *job.ExecutionMessage) *core.JobExecutionMessage {
	if msg == nil {
		return nil
	}
	return &core.JobExecutionMessage{
		JobID:          strings.TrimSpace(msg.JobID),
		ScriptPath:     strings.TrimSpace(msg.ScriptPath),
		Parameters:     copyAnyMap(msg.Parameters),
		IdempotencyKey: strings.TrimSpace(msg.IdempotencyKey),
		DedupPolicy:    strings.TrimSpace(string(msg.DedupPolicy)),
	}
}

// ToNackOptions maps go-services nack options to go-job.
func ToNackOptions(opts core.JobNackOptions) queue.NackOptions {
	return queue.NackOptions{
		Delay:      opts.Delay,
		Requeue:    opts.Requeue,
		DeadLetter: opts.DeadLetter,
		Reason:     opts.Reason,
	}
}

// FromNackOptions maps go-job nack options to go-services.
func FromNackOptions(opts queue.NackOptions) core.JobNackOptions {
	return core.JobNackOptions{
		Delay:      opts.Delay,
		Requeue:    opts.Requeue,
		DeadLetter: opts.DeadLetter,
		Reason:     opts.Reason,
	}
}

type EnqueuerAdapter struct {
	enqueuer queue.Enqueuer
}

func NewEnqueuerAdapter(enqueuer queue.Enqueuer) *EnqueuerAdapter {
	return &EnqueuerAdapter{enqueuer: enqueuer}
}

func (a *EnqueuerAdapter) Enqueue(ctx context.Context, msg *core.JobExecutionMessage) error {
	if a == nil || a.enqueuer == nil {
		return fmt.Errorf("gojob: enqueuer is not configured")
	}
	if msg == nil {
		return fmt.Errorf("gojob: execution message is required")
	}
	return a.enqueuer.Enqueue(ctx, ToExecutionMessage(msg))
}

type DeliveryAdapter struct {
	delivery queue.Delivery
	policy   RetryPolicy
}

func NewDeliveryAdapter(delivery queue.Delivery, policy RetryPolicy) *DeliveryAdapter {
	return &DeliveryAdapter{delivery: delivery, policy: policy}
}

func (d *DeliveryAdapter) Message() *core.JobExecutionMessage {
	if d == nil || d.delivery == nil {
		return nil
	}
	return FromExecutionMessage(d.delivery.Message())
}

func (d *DeliveryAdapter) Ack(ctx context.Context) error {
	if d == nil || d.delivery == nil {
		return fmt.Errorf("gojob: delivery is not configured")
	}
	return d.delivery.Ack(ctx)
}

func (d *DeliveryAdapter) Nack(ctx context.Context, opts core.JobNackOptions) error {
	return d.NackForAttempt(ctx, opts, 0)
}

func (d *DeliveryAdapter) NackForAttempt(ctx context.Context, opts core.JobNackOptions, attempt int) error {
	if d == nil || d.delivery == nil {
		return fmt.Errorf("gojob: delivery is not configured")
	}
	normalized := d.policy.NormalizeAttempt(opts, attempt)
	return d.delivery.Nack(ctx, ToNackOptions(normalized))
}

type DequeuerAdapter struct {
	dequeuer queue.Dequeuer
	policy   RetryPolicy
}

func NewDequeuerAdapter(dequeuer queue.Dequeuer, policy RetryPolicy) *DequeuerAdapter {
	return &DequeuerAdapter{dequeuer: dequeuer, policy: policy}
}

func (a *DequeuerAdapter) Dequeue(ctx context.Context) (core.JobDelivery, error) {
	if a == nil || a.dequeuer == nil {
		return nil, fmt.Errorf("gojob: dequeuer is not configured")
	}
	delivery, err := a.dequeuer.Dequeue(ctx)
	if err != nil {
		return nil, err
	}
	return NewDeliveryAdapter(delivery, a.policy), nil
}

type WorkerHookAdapter struct {
	hook core.JobWorkerHook
}

func NewWorkerHookAdapter(hook core.JobWorkerHook) *WorkerHookAdapter {
	return &WorkerHookAdapter{hook: hook}
}

func (a *WorkerHookAdapter) OnStart(ctx context.Context, event worker.Event) {
	if a == nil || a.hook == nil {
		return
	}
	a.hook.OnStart(ctx, mapWorkerEvent(event))
}

func (a *WorkerHookAdapter) OnSuccess(ctx context.Context, event worker.Event) {
	if a == nil || a.hook == nil {
		return
	}
	a.hook.OnSuccess(ctx, mapWorkerEvent(event))
}

func (a *WorkerHookAdapter) OnFailure(ctx context.Context, event worker.Event) {
	if a == nil || a.hook == nil {
		return
	}
	a.hook.OnFailure(ctx, mapWorkerEvent(event))
}

func (a *WorkerHookAdapter) OnRetry(ctx context.Context, event worker.Event) {
	if a == nil || a.hook == nil {
		return
	}
	a.hook.OnRetry(ctx, mapWorkerEvent(event))
}

func mapWorkerEvent(event worker.Event) core.JobWorkerEvent {
	message := event.Message
	if message == nil && event.Delivery != nil {
		message = event.Delivery.Message()
	}
	return core.JobWorkerEvent{
		Message:   FromExecutionMessage(message),
		Attempt:   event.Attempt,
		Delay:     event.Delay,
		Err:       event.Err,
		StartedAt: event.StartedAt,
		Duration:  event.Duration,
	}
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

var (
	_ core.JobEnqueuer   = (*EnqueuerAdapter)(nil)
	_ core.JobDelivery   = (*DeliveryAdapter)(nil)
	_ core.JobDequeuer   = (*DequeuerAdapter)(nil)
	_ worker.Hook        = (*WorkerHookAdapter)(nil)
	_ core.JobWorkerHook = (*capturingCoreHook)(nil)
)

// capturingCoreHook only exists to assert local compile-time compatibility.
type capturingCoreHook struct{}

func (capturingCoreHook) OnStart(context.Context, core.JobWorkerEvent)   {}
func (capturingCoreHook) OnSuccess(context.Context, core.JobWorkerEvent) {}
func (capturingCoreHook) OnFailure(context.Context, core.JobWorkerEvent) {}
func (capturingCoreHook) OnRetry(context.Context, core.JobWorkerEvent)   {}
