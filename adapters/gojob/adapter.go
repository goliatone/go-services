package gojob

import (
	"context"
	"fmt"
	"maps"
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
	if out.Disposition == "" {
		out.Disposition = core.JobNackDispositionRetry
	}
	if out.Delay < 0 {
		out.Delay = 0
	}
	if p.MaxDelay > 0 && out.Delay > p.MaxDelay {
		out.Delay = p.MaxDelay
	}
	if p.MaxAttempts > 0 && attempt >= p.MaxAttempts && out.Disposition == core.JobNackDispositionRetry {
		if p.DeadLetterOnMax {
			out.Disposition = core.JobNackDispositionDeadLetter
		} else {
			out.Disposition = core.JobNackDispositionFailed
		}
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
	disposition := queue.NackDisposition(strings.TrimSpace(string(opts.Disposition)))
	if disposition == "" {
		disposition = queue.NackDispositionRetry
	}
	return queue.NackOptions{
		Disposition: disposition,
		Delay:       opts.Delay,
		Reason:      opts.Reason,
	}
}

// FromNackOptions maps go-job nack options to go-services.
func FromNackOptions(opts queue.NackOptions) core.JobNackOptions {
	return core.JobNackOptions{
		Disposition: core.JobNackDisposition(strings.TrimSpace(string(opts.Disposition))),
		Delay:       opts.Delay,
		Reason:      opts.Reason,
	}
}

type EnqueuerAdapter struct {
	enqueuer queue.Enqueuer
}

func NewEnqueuerAdapter(enqueuer queue.Enqueuer) *EnqueuerAdapter {
	return &EnqueuerAdapter{enqueuer: enqueuer}
}

func (a *EnqueuerAdapter) Enqueue(ctx context.Context, msg *core.JobExecutionMessage) (core.JobEnqueueReceipt, error) {
	if a == nil || a.enqueuer == nil {
		return core.JobEnqueueReceipt{}, fmt.Errorf("gojob: enqueuer is not configured")
	}
	if msg == nil {
		return core.JobEnqueueReceipt{}, fmt.Errorf("gojob: execution message is required")
	}
	receipt, err := a.enqueuer.Enqueue(ctx, ToExecutionMessage(msg))
	if err != nil {
		return core.JobEnqueueReceipt{}, err
	}
	return toEnqueueReceipt(receipt), nil
}

func (a *EnqueuerAdapter) EnqueueAt(ctx context.Context, msg *core.JobExecutionMessage, at time.Time) (core.JobEnqueueReceipt, error) {
	if a == nil || a.enqueuer == nil {
		return core.JobEnqueueReceipt{}, fmt.Errorf("gojob: enqueuer is not configured")
	}
	if msg == nil {
		return core.JobEnqueueReceipt{}, fmt.Errorf("gojob: execution message is required")
	}
	scheduled, ok := a.enqueuer.(queue.ScheduledEnqueuer)
	if !ok {
		return core.JobEnqueueReceipt{}, fmt.Errorf("gojob: %w", queue.ErrScheduledEnqueueUnsupported)
	}
	receipt, err := scheduled.EnqueueAt(ctx, ToExecutionMessage(msg), at)
	if err != nil {
		return core.JobEnqueueReceipt{}, err
	}
	return toEnqueueReceipt(receipt), nil
}

func (a *EnqueuerAdapter) EnqueueAfter(ctx context.Context, msg *core.JobExecutionMessage, delay time.Duration) (core.JobEnqueueReceipt, error) {
	if a == nil || a.enqueuer == nil {
		return core.JobEnqueueReceipt{}, fmt.Errorf("gojob: enqueuer is not configured")
	}
	if msg == nil {
		return core.JobEnqueueReceipt{}, fmt.Errorf("gojob: execution message is required")
	}
	scheduled, ok := a.enqueuer.(queue.ScheduledEnqueuer)
	if !ok {
		return core.JobEnqueueReceipt{}, fmt.Errorf("gojob: %w", queue.ErrScheduledEnqueueUnsupported)
	}
	receipt, err := scheduled.EnqueueAfter(ctx, ToExecutionMessage(msg), delay)
	if err != nil {
		return core.JobEnqueueReceipt{}, err
	}
	return toEnqueueReceipt(receipt), nil
}

func (a *EnqueuerAdapter) GetDispatchStatus(ctx context.Context, dispatchID string) (core.JobDispatchStatus, error) {
	if a == nil || a.enqueuer == nil {
		return core.JobDispatchStatus{}, fmt.Errorf("gojob: enqueuer is not configured")
	}
	reader, ok := a.enqueuer.(queue.DispatchStatusReader)
	if !ok {
		return core.JobDispatchStatus{}, fmt.Errorf("gojob: %w", queue.ErrDispatchStatusUnsupported)
	}
	status, err := reader.GetDispatchStatus(ctx, strings.TrimSpace(dispatchID))
	if err != nil {
		return core.JobDispatchStatus{}, err
	}
	return FromDispatchStatus(status), nil
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
	maps.Copy(out, in)
	return out
}

func toEnqueueReceipt(receipt queue.EnqueueReceipt) core.JobEnqueueReceipt {
	return core.JobEnqueueReceipt{
		DispatchID: strings.TrimSpace(receipt.DispatchID),
		EnqueuedAt: receipt.EnqueuedAt.UTC(),
	}
}

// FromDispatchStatus maps go-job dispatch lifecycle status to go-services.
func FromDispatchStatus(status queue.DispatchStatus) core.JobDispatchStatus {
	return core.JobDispatchStatus{
		DispatchID:     strings.TrimSpace(status.DispatchID),
		State:          core.JobDispatchState(strings.TrimSpace(string(status.State))),
		Attempt:        status.Attempt,
		EnqueuedAt:     toUTCTimePointer(status.EnqueuedAt),
		UpdatedAt:      toUTCTimePointer(status.UpdatedAt),
		NextRunAt:      toUTCTimePointer(status.NextRunAt),
		TerminalReason: strings.TrimSpace(status.TerminalReason),
	}
}

func toUTCTimePointer(input *time.Time) *time.Time {
	if input == nil {
		return nil
	}
	value := input.UTC()
	return &value
}

var (
	_ core.JobEnqueuer             = (*EnqueuerAdapter)(nil)
	_ core.JobScheduledEnqueuer    = (*EnqueuerAdapter)(nil)
	_ core.JobDispatchStatusReader = (*EnqueuerAdapter)(nil)
	_ core.JobDelivery             = (*DeliveryAdapter)(nil)
	_ core.JobDequeuer             = (*DequeuerAdapter)(nil)
	_ worker.Hook                  = (*WorkerHookAdapter)(nil)
	_ core.JobWorkerHook           = (*capturingCoreHook)(nil)
)

// capturingCoreHook only exists to assert local compile-time compatibility.
type capturingCoreHook struct{}

func (capturingCoreHook) OnStart(context.Context, core.JobWorkerEvent)   {}
func (capturingCoreHook) OnSuccess(context.Context, core.JobWorkerEvent) {}
func (capturingCoreHook) OnFailure(context.Context, core.JobWorkerEvent) {}
func (capturingCoreHook) OnRetry(context.Context, core.JobWorkerEvent)   {}
