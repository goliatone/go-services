package sqlstore

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	repositorycache "github.com/goliatone/go-repository-cache/cache"
	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/ratelimit"
)

const rateLimitStateCacheKeyPrefix = "go-services::ratelimit_state::v1"

type CachedRateLimitStateStore struct {
	base  ratelimit.StateStore
	cache repositorycache.CacheService
}

func NewCachedRateLimitStateStore(
	base ratelimit.StateStore,
	cacheService repositorycache.CacheService,
) (*CachedRateLimitStateStore, error) {
	if base == nil {
		return nil, fmt.Errorf("sqlstore: base rate-limit state store is required")
	}
	if cacheService == nil {
		return nil, fmt.Errorf("sqlstore: rate-limit cache service is required")
	}
	return &CachedRateLimitStateStore{base: base, cache: cacheService}, nil
}

// RateLimitStateCacheKey returns the deterministic cache key contract for
// rate-limit state reads: go-services::ratelimit_state::v1::<provider>::<scope_type>::<scope_id>::<bucket_key>
// with each segment URL-path escaped after key normalization.
func RateLimitStateCacheKey(key core.RateLimitKey) (string, error) {
	normalized := normalizeRateLimitKey(key)
	if err := validateRateLimitKey(normalized); err != nil {
		return "", err
	}
	segments := []string{
		normalized.ProviderID,
		normalized.ScopeType,
		normalized.ScopeID,
		normalized.BucketKey,
	}
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(append([]string{rateLimitStateCacheKeyPrefix}, segments...), "::"), nil
}

func (s *CachedRateLimitStateStore) Get(ctx context.Context, key core.RateLimitKey) (ratelimit.State, error) {
	if s == nil || s.base == nil || s.cache == nil {
		return ratelimit.State{}, fmt.Errorf("sqlstore: cached rate-limit state store is not configured")
	}
	normalized := normalizeRateLimitKey(key)
	cacheKey, err := RateLimitStateCacheKey(normalized)
	if err != nil {
		return ratelimit.State{}, err
	}

	state, err := repositorycache.GetOrFetch(ctx, s.cache, cacheKey, func(ctx context.Context) (ratelimit.State, error) {
		fetched, fetchErr := s.base.Get(ctx, normalized)
		if fetchErr != nil {
			return ratelimit.State{}, fetchErr
		}
		fetched.Key = normalizeRateLimitKey(fetched.Key)
		return cloneRateLimitState(fetched), nil
	})
	if err != nil {
		return ratelimit.State{}, err
	}
	return cloneRateLimitState(state), nil
}

func (s *CachedRateLimitStateStore) Upsert(ctx context.Context, state ratelimit.State) error {
	if s == nil || s.base == nil || s.cache == nil {
		return fmt.Errorf("sqlstore: cached rate-limit state store is not configured")
	}
	state.Key = normalizeRateLimitKey(state.Key)
	if err := validateRateLimitKey(state.Key); err != nil {
		return err
	}
	state.Metadata = copyAnyMap(state.Metadata)

	if err := s.base.Upsert(ctx, state); err != nil {
		return err
	}

	cacheKey, err := RateLimitStateCacheKey(state.Key)
	if err != nil {
		return err
	}
	if err := s.cache.Delete(ctx, cacheKey); err != nil {
		return err
	}
	return nil
}

func cloneRateLimitState(state ratelimit.State) ratelimit.State {
	cloned := state
	cloned.Key = normalizeRateLimitKey(state.Key)
	cloned.Metadata = copyAnyMap(state.Metadata)
	cloned.ResetAt = cloneTimePointer(state.ResetAt)
	cloned.ThrottledUntil = cloneTimePointer(state.ThrottledUntil)
	cloned.RetryAfter = cloneDurationPointer(state.RetryAfter)
	return cloned
}

func cloneTimePointer(input *time.Time) *time.Time {
	if input == nil {
		return nil
	}
	value := input.UTC()
	return &value
}

func cloneDurationPointer(input *time.Duration) *time.Duration {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}

var _ ratelimit.StateStore = (*CachedRateLimitStateStore)(nil)
