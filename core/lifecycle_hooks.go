package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type LifecycleHookCoordinator struct {
	mu         sync.RWMutex
	preCommit  []LifecycleHook
	postCommit []LifecycleHook
}

func NewLifecycleHookCoordinator() *LifecycleHookCoordinator {
	return &LifecycleHookCoordinator{
		preCommit:  make([]LifecycleHook, 0),
		postCommit: make([]LifecycleHook, 0),
	}
}

func (c *LifecycleHookCoordinator) RegisterPreCommit(hook LifecycleHook) {
	if c == nil || hook == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.preCommit = append(c.preCommit, hook)
}

func (c *LifecycleHookCoordinator) RegisterPostCommit(hook LifecycleHook) {
	if c == nil || hook == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.postCommit = append(c.postCommit, hook)
}

// ExecutePreCommit runs strict hooks synchronously in registration order.
// The first hook error is returned to allow callers to fail the transaction.
func (c *LifecycleHookCoordinator) ExecutePreCommit(ctx context.Context, event LifecycleEvent) error {
	for _, hook := range c.preHooks() {
		if hook == nil {
			continue
		}
		if err := hook.OnEvent(ctx, event); err != nil {
			return fmt.Errorf("core: pre-commit lifecycle hook %q failed: %w", hookName(hook), err)
		}
	}
	return nil
}

// ExecutePreCommitAndEnqueue keeps synchronous lifecycle guarantees explicit:
// if pre-commit hooks fail, the outbox event is not enqueued.
func (c *LifecycleHookCoordinator) ExecutePreCommitAndEnqueue(
	ctx context.Context,
	event LifecycleEvent,
	outbox OutboxStore,
) error {
	if outbox == nil {
		return fmt.Errorf("core: outbox store is required")
	}
	if err := c.ExecutePreCommit(ctx, event); err != nil {
		return err
	}
	if err := outbox.Enqueue(ctx, event); err != nil {
		return fmt.Errorf("core: enqueue lifecycle outbox event failed: %w", err)
	}
	return nil
}

// ExecutePostCommit runs non-transactional hooks after commit semantics.
// Failures are aggregated and returned for observability without implying rollback.
func (c *LifecycleHookCoordinator) ExecutePostCommit(ctx context.Context, event LifecycleEvent) error {
	var hookErr error
	for _, hook := range c.postHooks() {
		if hook == nil {
			continue
		}
		if err := hook.OnEvent(ctx, event); err != nil {
			hookErr = errors.Join(hookErr, fmt.Errorf("post-commit lifecycle hook %q failed: %w", hookName(hook), err))
		}
	}
	return hookErr
}

func (c *LifecycleHookCoordinator) preHooks() []LifecycleHook {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]LifecycleHook, len(c.preCommit))
	copy(out, c.preCommit)
	return out
}

func (c *LifecycleHookCoordinator) postHooks() []LifecycleHook {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]LifecycleHook, len(c.postCommit))
	copy(out, c.postCommit)
	return out
}

func hookName(hook LifecycleHook) string {
	if hook == nil {
		return "unknown"
	}
	name := strings.TrimSpace(hook.Name())
	if name == "" {
		return "unnamed"
	}
	return name
}
