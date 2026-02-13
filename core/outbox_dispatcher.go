package core

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const MetadataKeyOutboxAttempts = "_outbox_attempts"

type OutboxDispatcherConfig struct {
	BatchSize      int
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func DefaultOutboxDispatcherConfig() OutboxDispatcherConfig {
	return OutboxDispatcherConfig{
		BatchSize:      50,
		MaxAttempts:    5,
		InitialBackoff: 2 * time.Second,
		MaxBackoff:     5 * time.Minute,
	}
}

type OutboxDispatcher struct {
	store    OutboxStore
	registry ProjectorRegistry
	config   OutboxDispatcherConfig
	now      func() time.Time
}

func NewOutboxDispatcher(
	store OutboxStore,
	registry ProjectorRegistry,
	config OutboxDispatcherConfig,
) (*OutboxDispatcher, error) {
	if store == nil {
		return nil, fmt.Errorf("core: outbox store is required")
	}
	if config.BatchSize <= 0 {
		config.BatchSize = DefaultOutboxDispatcherConfig().BatchSize
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultOutboxDispatcherConfig().MaxAttempts
	}
	if config.InitialBackoff <= 0 {
		config.InitialBackoff = DefaultOutboxDispatcherConfig().InitialBackoff
	}
	if config.MaxBackoff <= 0 {
		config.MaxBackoff = DefaultOutboxDispatcherConfig().MaxBackoff
	}
	return &OutboxDispatcher{
		store:    store,
		registry: registry,
		config:   config,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}, nil
}

func (d *OutboxDispatcher) DispatchPending(ctx context.Context, batchSize int) (DispatchStats, error) {
	if d == nil || d.store == nil {
		return DispatchStats{}, fmt.Errorf("core: outbox dispatcher is not configured")
	}
	limit := batchSize
	if limit <= 0 {
		limit = d.config.BatchSize
	}
	events, err := d.store.ClaimBatch(ctx, limit)
	if err != nil {
		return DispatchStats{}, err
	}

	stats := DispatchStats{Claimed: len(events)}
	var dispatchErr error
	for _, event := range events {
		if err := d.dispatchOne(ctx, event); err != nil {
			if retryErr := d.retryEvent(ctx, event, err); retryErr != nil {
				dispatchErr = joinErrors(dispatchErr, retryErr)
			}
			if nextAttemptIndex(event)+1 >= d.config.MaxAttempts {
				stats.Failed++
			} else {
				stats.Retried++
			}
			dispatchErr = joinErrors(dispatchErr, err)
			continue
		}
		if err := d.store.Ack(ctx, strings.TrimSpace(event.ID)); err != nil {
			dispatchErr = joinErrors(dispatchErr, err)
			continue
		}
		stats.Delivered++
	}

	return stats, dispatchErr
}

func (d *OutboxDispatcher) dispatchOne(ctx context.Context, event LifecycleEvent) error {
	if d == nil || d.registry == nil {
		return nil
	}
	handlers := d.registry.Handlers()
	for i, handler := range handlers {
		if handler == nil {
			continue
		}
		if err := handler.Handle(ctx, event); err != nil {
			return fmt.Errorf("core: lifecycle projector %d failed for event %q: %w", i, event.ID, err)
		}
	}
	return nil
}

func (d *OutboxDispatcher) retryEvent(ctx context.Context, event LifecycleEvent, cause error) error {
	attempt := nextAttemptIndex(event)
	if attempt+1 >= d.config.MaxAttempts {
		return d.store.Retry(ctx, strings.TrimSpace(event.ID), cause, time.Time{})
	}
	nextAttemptAt := d.now().Add(d.nextBackoffDelay(attempt + 1))
	return d.store.Retry(ctx, strings.TrimSpace(event.ID), cause, nextAttemptAt)
}

func (d *OutboxDispatcher) nextBackoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := float64(d.config.InitialBackoff)
	multiplier := math.Pow(2, float64(attempt-1))
	next := time.Duration(base * multiplier)
	if next < 0 {
		return d.config.MaxBackoff
	}
	if next > d.config.MaxBackoff {
		return d.config.MaxBackoff
	}
	return next
}

func nextAttemptIndex(event LifecycleEvent) int {
	if len(event.Metadata) == 0 {
		return 0
	}
	raw, ok := event.Metadata[MetadataKeyOutboxAttempts]
	if !ok {
		return 0
	}
	switch typed := raw.(type) {
	case int:
		if typed < 0 {
			return 0
		}
		return typed
	case int64:
		if typed < 0 {
			return 0
		}
		return int(typed)
	case float64:
		if typed < 0 {
			return 0
		}
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil && parsed >= 0 {
			return parsed
		}
	}
	return 0
}

func joinErrors(existing error, next error) error {
	if existing == nil {
		return next
	}
	if next == nil {
		return existing
	}
	return fmt.Errorf("%w; %v", existing, next)
}

var _ LifecycleDispatcher = (*OutboxDispatcher)(nil)
