package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestAdaptivePolicy_BeforeCallAllowsWhenNoState(t *testing.T) {
	policy := NewAdaptivePolicy(NewMemoryStateStore())

	err := policy.BeforeCall(context.Background(), core.RateLimitKey{ProviderID: "github", ScopeType: "user", ScopeID: "usr_1", BucketKey: "api"})
	if err != nil {
		t.Fatalf("expected no error when no state exists, got %v", err)
	}
}

func TestAdaptivePolicy_AfterCallParsesHeadersAndPersistsState(t *testing.T) {
	store := NewMemoryStateStore()
	policy := NewAdaptivePolicy(store)
	now := time.Unix(1_700_000_000, 0).UTC()
	policy.Now = func() time.Time { return now }

	key := core.RateLimitKey{ProviderID: "github", ScopeType: "user", ScopeID: "usr_1", BucketKey: "api"}
	resetAt := now.Add(45 * time.Second)
	err := policy.AfterCall(context.Background(), key, core.ProviderResponseMeta{
		StatusCode: 200,
		Headers: map[string]string{
			"X-RateLimit-Limit":     "5000",
			"X-RateLimit-Remaining": "4999",
			"X-RateLimit-Reset":     "1700000045",
		},
		Metadata: map[string]any{"endpoint": "issues"},
	})
	if err != nil {
		t.Fatalf("after call: %v", err)
	}

	state, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.Limit != 5000 {
		t.Fatalf("expected limit 5000, got %d", state.Limit)
	}
	if state.Remaining != 4999 {
		t.Fatalf("expected remaining 4999, got %d", state.Remaining)
	}
	if state.ResetAt == nil || !state.ResetAt.Equal(resetAt) {
		t.Fatalf("expected reset at %s, got %+v", resetAt, state.ResetAt)
	}
	if state.Metadata["endpoint"] != "issues" {
		t.Fatalf("expected metadata to include endpoint")
	}
}

func TestAdaptivePolicy_BlocksWhenThrottleWindowIsActive(t *testing.T) {
	store := NewMemoryStateStore()
	policy := NewAdaptivePolicy(store)
	now := time.Unix(1_700_000_000, 0).UTC()
	policy.Now = func() time.Time { return now }

	key := core.RateLimitKey{ProviderID: "github", ScopeType: "user", ScopeID: "usr_1", BucketKey: "api"}
	until := now.Add(20 * time.Second)
	if err := store.Upsert(context.Background(), State{Key: key, ThrottledUntil: &until, Remaining: 0}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	err := policy.BeforeCall(context.Background(), key)
	if err == nil {
		t.Fatalf("expected throttle error")
	}
	var throttledErr ThrottledError
	if !errors.As(err, &throttledErr) {
		t.Fatalf("expected ThrottledError, got %T", err)
	}
	if throttledErr.RetryAfter <= 0 {
		t.Fatalf("expected retry_after > 0")
	}
}

func TestAdaptivePolicy_AfterCall429UsesRetryAfterAndAttempts(t *testing.T) {
	store := NewMemoryStateStore()
	policy := NewAdaptivePolicy(store)
	now := time.Unix(1_700_000_000, 0).UTC()
	policy.Now = func() time.Time { return now }

	key := core.RateLimitKey{ProviderID: "github", ScopeType: "user", ScopeID: "usr_1", BucketKey: "api"}
	if err := policy.AfterCall(context.Background(), key, core.ProviderResponseMeta{
		StatusCode: 429,
		Headers: map[string]string{
			"Retry-After": "10",
		},
	}); err != nil {
		t.Fatalf("after call throttled: %v", err)
	}

	state, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.Attempts != 1 {
		t.Fatalf("expected attempts 1, got %d", state.Attempts)
	}
	if state.ThrottledUntil == nil {
		t.Fatalf("expected throttled_until")
	}
	if got := state.ThrottledUntil.Sub(now); got != 10*time.Second {
		t.Fatalf("expected throttled window of 10s, got %s", got)
	}
	if state.RetryAfter == nil || *state.RetryAfter != 10*time.Second {
		t.Fatalf("expected retry_after 10s")
	}
}

func TestAdaptivePolicy_AdaptiveBackoffWithoutRetryAfter(t *testing.T) {
	store := NewMemoryStateStore()
	policy := NewAdaptivePolicy(store)
	policy.InitialBackoff = 2 * time.Second
	policy.MaxBackoff = 30 * time.Second
	now := time.Unix(1_700_000_000, 0).UTC()
	policy.Now = func() time.Time { return now }

	key := core.RateLimitKey{ProviderID: "github", ScopeType: "user", ScopeID: "usr_1", BucketKey: "api"}
	if err := policy.AfterCall(context.Background(), key, core.ProviderResponseMeta{StatusCode: 429}); err != nil {
		t.Fatalf("first throttled call: %v", err)
	}

	now = now.Add(3 * time.Second)
	if err := policy.AfterCall(context.Background(), key, core.ProviderResponseMeta{StatusCode: 429}); err != nil {
		t.Fatalf("second throttled call: %v", err)
	}

	state, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.Attempts != 2 {
		t.Fatalf("expected attempts 2, got %d", state.Attempts)
	}
	if state.ThrottledUntil == nil {
		t.Fatalf("expected throttled_until")
	}
	if got := state.ThrottledUntil.Sub(now); got != 4*time.Second {
		t.Fatalf("expected adaptive delay of 4s, got %s", got)
	}
}

func TestAdaptivePolicy_ResetsAttemptsOnSuccessfulCall(t *testing.T) {
	store := NewMemoryStateStore()
	policy := NewAdaptivePolicy(store)
	now := time.Unix(1_700_000_000, 0).UTC()
	policy.Now = func() time.Time { return now }

	key := core.RateLimitKey{ProviderID: "github", ScopeType: "user", ScopeID: "usr_1", BucketKey: "api"}
	if err := store.Upsert(context.Background(), State{
		Key:      key,
		Attempts: 3,
		ThrottledUntil: func() *time.Time {
			value := now.Add(10 * time.Second)
			return &value
		}(),
	}); err != nil {
		t.Fatalf("seed throttled state: %v", err)
	}

	now = now.Add(12 * time.Second)
	if err := policy.AfterCall(context.Background(), key, core.ProviderResponseMeta{StatusCode: 200}); err != nil {
		t.Fatalf("after successful call: %v", err)
	}

	state, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.Attempts != 0 {
		t.Fatalf("expected attempts reset to zero, got %d", state.Attempts)
	}
	if state.ThrottledUntil != nil {
		t.Fatalf("expected throttle window cleared")
	}
}
