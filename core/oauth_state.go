package core

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"
)

const defaultOAuthStateTTL = 15 * time.Minute
const defaultOAuthStateMaxEntries = 4096

type OAuthStateRecord struct {
	State           string
	ProviderID      string
	Scope           ScopeRef
	RedirectURI     string
	RequestedGrants []string
	Metadata        map[string]any
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type OAuthStateStore interface {
	Save(ctx context.Context, record OAuthStateRecord) error
	Consume(ctx context.Context, state string) (OAuthStateRecord, error)
}

type MemoryOAuthStateStore struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	entries    map[string]OAuthStateRecord
}

func NewMemoryOAuthStateStore(ttl time.Duration) *MemoryOAuthStateStore {
	return NewMemoryOAuthStateStoreWithLimits(ttl, defaultOAuthStateMaxEntries)
}

func NewMemoryOAuthStateStoreWithLimits(ttl time.Duration, maxEntries int) *MemoryOAuthStateStore {
	if ttl <= 0 {
		ttl = defaultOAuthStateTTL
	}
	if maxEntries <= 0 {
		maxEntries = defaultOAuthStateMaxEntries
	}
	return &MemoryOAuthStateStore{
		ttl:        ttl,
		maxEntries: maxEntries,
		entries:    map[string]OAuthStateRecord{},
	}
}

func (s *MemoryOAuthStateStore) Save(_ context.Context, record OAuthStateRecord) error {
	if s == nil {
		return fmt.Errorf("core: oauth state store is not configured")
	}
	state := strings.TrimSpace(record.State)
	if state == "" {
		return fmt.Errorf("core: oauth state is required")
	}

	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.CreatedAt.Add(s.ttl)
	}

	s.mu.Lock()
	s.pruneExpiredLocked(now)
	if _, exists := s.entries[state]; !exists {
		s.enforceCapacityLocked(now, 1)
	}
	s.entries[state] = cloneOAuthStateRecord(record)
	s.mu.Unlock()

	return nil
}

func (s *MemoryOAuthStateStore) Consume(_ context.Context, state string) (OAuthStateRecord, error) {
	if s == nil {
		return OAuthStateRecord{}, fmt.Errorf("core: oauth state store is not configured")
	}
	state = strings.TrimSpace(state)
	if state == "" {
		return OAuthStateRecord{}, fmt.Errorf("core: oauth state is required")
	}

	now := time.Now().UTC()
	s.mu.Lock()
	s.pruneExpiredLocked(now)
	record, ok := s.entries[state]
	if ok && !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
		delete(s.entries, state)
		ok = false
	}
	if ok {
		delete(s.entries, state)
	}
	s.mu.Unlock()

	if !ok {
		return OAuthStateRecord{}, fmt.Errorf("core: oauth state not found")
	}
	return cloneOAuthStateRecord(record), nil
}

func (s *MemoryOAuthStateStore) pruneExpiredLocked(now time.Time) {
	for key, record := range s.entries {
		if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
			delete(s.entries, key)
		}
	}
}

func (s *MemoryOAuthStateStore) enforceCapacityLocked(now time.Time, incoming int) {
	if s.maxEntries <= 0 {
		return
	}
	target := s.maxEntries - incoming
	if target < 0 {
		target = 0
	}
	for len(s.entries) > target {
		s.evictOldestLocked(now)
	}
}

func (s *MemoryOAuthStateStore) evictOldestLocked(now time.Time) {
	var oldestKey string
	var oldestAt time.Time
	for key, record := range s.entries {
		candidateAt := record.ExpiresAt
		if candidateAt.IsZero() {
			if record.CreatedAt.IsZero() {
				candidateAt = now
			} else {
				candidateAt = record.CreatedAt.Add(s.ttl)
			}
		}
		if oldestKey == "" || candidateAt.Before(oldestAt) {
			oldestKey = key
			oldestAt = candidateAt
		}
	}
	if oldestKey != "" {
		delete(s.entries, oldestKey)
	}
}

func generateOAuthState() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("core: generate oauth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func cloneOAuthStateRecord(record OAuthStateRecord) OAuthStateRecord {
	cloned := record
	cloned.RequestedGrants = append([]string(nil), record.RequestedGrants...)
	if record.Metadata == nil {
		cloned.Metadata = map[string]any{}
	} else {
		cloned.Metadata = copyAnyMap(record.Metadata)
	}
	return cloned
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
