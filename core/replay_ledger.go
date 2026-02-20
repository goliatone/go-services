package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const defaultReplayLedgerTTL = 5 * time.Minute
const defaultReplayLedgerMaxEntries = 8192

type MemoryReplayLedger struct {
	mu         sync.Mutex
	defaultTTL time.Duration
	maxEntries int
	entries    map[string]time.Time
	Now        func() time.Time
}

func NewMemoryReplayLedger(defaultTTL time.Duration) *MemoryReplayLedger {
	return NewMemoryReplayLedgerWithLimits(defaultTTL, defaultReplayLedgerMaxEntries)
}

func NewMemoryReplayLedgerWithLimits(defaultTTL time.Duration, maxEntries int) *MemoryReplayLedger {
	if defaultTTL <= 0 {
		defaultTTL = defaultReplayLedgerTTL
	}
	if maxEntries <= 0 {
		maxEntries = defaultReplayLedgerMaxEntries
	}
	return &MemoryReplayLedger{
		defaultTTL: defaultTTL,
		maxEntries: maxEntries,
		entries:    map[string]time.Time{},
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (l *MemoryReplayLedger) Claim(_ context.Context, key string, ttl time.Duration) (bool, error) {
	if l == nil {
		return false, fmt.Errorf("core: replay ledger is not configured")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false, fmt.Errorf("core: replay key is required")
	}
	if ttl <= 0 {
		ttl = l.defaultTTL
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneExpiredLocked(now)
	if expiresAt, ok := l.entries[key]; ok {
		if now.Before(expiresAt) {
			return false, nil
		}
		delete(l.entries, key)
	}
	l.enforceCapacityLocked(now, 1)
	l.entries[key] = now.Add(ttl)
	return true, nil
}

func (l *MemoryReplayLedger) PurgeExpired(_ context.Context) (int, error) {
	if l == nil {
		return 0, fmt.Errorf("core: replay ledger is not configured")
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	pruned := 0
	for key, expiresAt := range l.entries {
		if !now.Before(expiresAt) {
			delete(l.entries, key)
			pruned++
		}
	}
	return pruned, nil
}

func (l *MemoryReplayLedger) now() time.Time {
	if l != nil && l.Now != nil {
		return l.Now().UTC()
	}
	return time.Now().UTC()
}

func (l *MemoryReplayLedger) pruneExpiredLocked(now time.Time) {
	for key, expiresAt := range l.entries {
		if !now.Before(expiresAt) {
			delete(l.entries, key)
		}
	}
}

func (l *MemoryReplayLedger) enforceCapacityLocked(now time.Time, incoming int) {
	if l.maxEntries <= 0 {
		return
	}
	target := l.maxEntries - incoming
	if target < 0 {
		target = 0
	}
	for len(l.entries) > target {
		l.evictOldestLocked(now)
	}
}

func (l *MemoryReplayLedger) evictOldestLocked(now time.Time) {
	var oldestKey string
	var oldestExpiry time.Time
	for key, expiry := range l.entries {
		if oldestKey == "" || expiry.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = expiry
		}
	}
	if oldestKey != "" {
		delete(l.entries, oldestKey)
		return
	}
	for key := range l.entries {
		delete(l.entries, key)
		break
	}
	_ = now
}

var _ ReplayLedger = (*MemoryReplayLedger)(nil)
