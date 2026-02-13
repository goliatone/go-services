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
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]OAuthStateRecord
}

func NewMemoryOAuthStateStore(ttl time.Duration) *MemoryOAuthStateStore {
	if ttl <= 0 {
		ttl = defaultOAuthStateTTL
	}
	return &MemoryOAuthStateStore{
		ttl:     ttl,
		entries: map[string]OAuthStateRecord{},
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

	s.mu.Lock()
	record, ok := s.entries[state]
	if ok {
		delete(s.entries, state)
	}
	s.mu.Unlock()

	if !ok {
		return OAuthStateRecord{}, fmt.Errorf("core: oauth state not found")
	}
	if !record.ExpiresAt.IsZero() && time.Now().UTC().After(record.ExpiresAt) {
		return OAuthStateRecord{}, fmt.Errorf("core: oauth state expired")
	}

	return cloneOAuthStateRecord(record), nil
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
