package core

import (
	"context"
	"testing"
	"time"
)

func TestMemoryOAuthStateStore_SavePrunesExpiredEntries(t *testing.T) {
	store := NewMemoryOAuthStateStoreWithLimits(time.Minute, 8)
	now := time.Now().UTC()

	if err := store.Save(context.Background(), OAuthStateRecord{
		State:     "stale_state",
		CreatedAt: now.Add(-2 * time.Minute),
		ExpiresAt: now.Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("save stale state: %v", err)
	}
	if err := store.Save(context.Background(), OAuthStateRecord{
		State:     "fresh_state",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("save fresh state: %v", err)
	}

	if _, err := store.Consume(context.Background(), "stale_state"); err == nil {
		t.Fatalf("expected stale state to be pruned and unavailable")
	}
	if _, err := store.Consume(context.Background(), "fresh_state"); err != nil {
		t.Fatalf("expected fresh state to remain available, got %v", err)
	}
}

func TestMemoryOAuthStateStore_SaveEnforcesMaxEntries(t *testing.T) {
	store := NewMemoryOAuthStateStoreWithLimits(time.Hour, 2)
	now := time.Now().UTC()

	if err := store.Save(context.Background(), OAuthStateRecord{
		State:     "state_a",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("save state_a: %v", err)
	}
	if err := store.Save(context.Background(), OAuthStateRecord{
		State:     "state_b",
		CreatedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("save state_b: %v", err)
	}
	if err := store.Save(context.Background(), OAuthStateRecord{
		State:     "state_c",
		CreatedAt: now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("save state_c: %v", err)
	}

	if _, err := store.Consume(context.Background(), "state_a"); err == nil {
		t.Fatalf("expected oldest state to be evicted when capacity is exceeded")
	}
	if _, err := store.Consume(context.Background(), "state_b"); err != nil {
		t.Fatalf("expected state_b to remain after eviction, got %v", err)
	}
	if _, err := store.Consume(context.Background(), "state_c"); err != nil {
		t.Fatalf("expected state_c to remain after eviction, got %v", err)
	}
}
