package sqlstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	repositorycache "github.com/goliatone/go-repository-cache/cache"
	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/ratelimit"
)

type stubRateLimitStateStore struct {
	mu          sync.Mutex
	state       ratelimit.State
	getCalls    int
	upsertCalls int
	getErr      error
	upsertErr   error
}

func (s *stubRateLimitStateStore) Get(_ context.Context, _ core.RateLimitKey) (ratelimit.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls++
	if s.getErr != nil {
		return ratelimit.State{}, s.getErr
	}
	return cloneRateLimitState(s.state), nil
}

func (s *stubRateLimitStateStore) Upsert(_ context.Context, state ratelimit.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsertCalls++
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.state = cloneRateLimitState(state)
	return nil
}

func TestCachedRateLimitStateStore_Get_MissFetchThenHit(t *testing.T) {
	cacheService := newTestRateLimitCacheService(t)
	base := &stubRateLimitStateStore{
		state: ratelimit.State{
			Key: core.RateLimitKey{
				ProviderID: "github",
				ScopeType:  "org",
				ScopeID:    "org_cache_1",
				BucketKey:  "api",
			},
			Limit:     5000,
			Remaining: 4999,
			UpdatedAt: time.Now().UTC(),
			Metadata:  map[string]any{"source": "base"},
		},
	}

	store, err := NewCachedRateLimitStateStore(base, cacheService)
	if err != nil {
		t.Fatalf("new cached state store: %v", err)
	}

	key := core.RateLimitKey{ProviderID: "github", ScopeType: "org", ScopeID: "org_cache_1", BucketKey: "api"}
	if _, err := store.Get(context.Background(), key); err != nil {
		t.Fatalf("first get: %v", err)
	}
	if base.getCalls != 1 {
		t.Fatalf("expected first get to fetch base store once, got %d", base.getCalls)
	}

	if _, err := store.Get(context.Background(), key); err != nil {
		t.Fatalf("second get: %v", err)
	}
	if base.getCalls != 1 {
		t.Fatalf("expected second get to be cache hit, base get calls=%d", base.getCalls)
	}
}

func TestCachedRateLimitStateStore_Upsert_InvalidatesCachedKey(t *testing.T) {
	cacheService := newTestRateLimitCacheService(t)
	base := &stubRateLimitStateStore{
		state: ratelimit.State{
			Key: core.RateLimitKey{
				ProviderID: "github",
				ScopeType:  "org",
				ScopeID:    "org_cache_2",
				BucketKey:  "api",
			},
			Limit:     5000,
			Remaining: 4999,
			UpdatedAt: time.Now().UTC(),
		},
	}

	store, err := NewCachedRateLimitStateStore(base, cacheService)
	if err != nil {
		t.Fatalf("new cached state store: %v", err)
	}

	key := core.RateLimitKey{ProviderID: "github", ScopeType: "org", ScopeID: "org_cache_2", BucketKey: "api"}
	if _, err := store.Get(context.Background(), key); err != nil {
		t.Fatalf("prime cache with get: %v", err)
	}
	if base.getCalls != 1 {
		t.Fatalf("expected one base read after cache prime, got %d", base.getCalls)
	}

	if err := store.Upsert(context.Background(), ratelimit.State{
		Key:       key,
		Limit:     5000,
		Remaining: 4500,
		UpdatedAt: time.Now().UTC(),
		Metadata:  map[string]any{"updated": true},
	}); err != nil {
		t.Fatalf("upsert through cached store: %v", err)
	}
	if base.upsertCalls != 1 {
		t.Fatalf("expected base upsert call count=1, got %d", base.upsertCalls)
	}

	state, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get after upsert invalidation: %v", err)
	}
	if base.getCalls != 2 {
		t.Fatalf("expected invalidated key to force second base read, got %d", base.getCalls)
	}
	if state.Remaining != 4500 {
		t.Fatalf("expected refreshed state remaining=4500, got %d", state.Remaining)
	}
}

func TestCachedRateLimitStateStore_KeyNormalizationUsesSingleCacheEntry(t *testing.T) {
	cacheService := newTestRateLimitCacheService(t)
	base := &stubRateLimitStateStore{
		state: ratelimit.State{
			Key: core.RateLimitKey{
				ProviderID: "github",
				ScopeType:  "org",
				ScopeID:    "ORG_KEY_3",
				BucketKey:  "api",
			},
			Limit:     5000,
			Remaining: 4998,
			UpdatedAt: time.Now().UTC(),
		},
	}
	store, err := NewCachedRateLimitStateStore(base, cacheService)
	if err != nil {
		t.Fatalf("new cached state store: %v", err)
	}

	first := core.RateLimitKey{
		ProviderID: " GitHub ",
		ScopeType:  " ORG ",
		ScopeID:    "ORG_KEY_3",
		BucketKey:  " API ",
	}
	second := core.RateLimitKey{
		ProviderID: "github",
		ScopeType:  "org",
		ScopeID:    "ORG_KEY_3",
		BucketKey:  "api",
	}

	if _, err := store.Get(context.Background(), first); err != nil {
		t.Fatalf("first normalized get: %v", err)
	}
	if _, err := store.Get(context.Background(), second); err != nil {
		t.Fatalf("second normalized get: %v", err)
	}
	if base.getCalls != 1 {
		t.Fatalf("expected normalized keys to share cache entry, base get calls=%d", base.getCalls)
	}

	firstCacheKey, err := RateLimitStateCacheKey(first)
	if err != nil {
		t.Fatalf("cache key for first input: %v", err)
	}
	secondCacheKey, err := RateLimitStateCacheKey(second)
	if err != nil {
		t.Fatalf("cache key for second input: %v", err)
	}
	if firstCacheKey != secondCacheKey {
		t.Fatalf("expected normalized cache keys to match, got %q != %q", firstCacheKey, secondCacheKey)
	}
}

func TestRateLimitStateCacheKey_Contract(t *testing.T) {
	key, err := RateLimitStateCacheKey(core.RateLimitKey{
		ProviderID: " GitHub ",
		ScopeType:  " ORG ",
		ScopeID:    "Org/Alpha Team",
		BucketKey:  " API:V1 ",
	})
	if err != nil {
		t.Fatalf("build cache key: %v", err)
	}

	const expected = "go-services::ratelimit_state::v1::github::org::Org%2FAlpha%20Team::api:v1"
	if key != expected {
		t.Fatalf("unexpected cache key contract: got %q want %q", key, expected)
	}
}

func TestCachedRateLimitStateStore_PropagatesBaseErrors(t *testing.T) {
	cacheService := newTestRateLimitCacheService(t)
	base := &stubRateLimitStateStore{getErr: ratelimit.ErrStateNotFound}
	store, err := NewCachedRateLimitStateStore(base, cacheService)
	if err != nil {
		t.Fatalf("new cached state store: %v", err)
	}

	_, err = store.Get(context.Background(), core.RateLimitKey{
		ProviderID: "github",
		ScopeType:  "org",
		ScopeID:    "org_cache_404",
		BucketKey:  "api",
	})
	if !errors.Is(err, ratelimit.ErrStateNotFound) {
		t.Fatalf("expected base error propagation, got %v", err)
	}
}

func newTestRateLimitCacheService(t *testing.T) repositorycache.CacheService {
	t.Helper()
	config := repositorycache.DefaultConfig()
	config.TTL = time.Minute
	service, err := repositorycache.NewCacheService(config)
	if err != nil {
		t.Fatalf("new cache service: %v", err)
	}
	return service
}
