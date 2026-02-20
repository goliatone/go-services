package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

var ErrStateNotFound = errors.New("ratelimit: state not found")

type State struct {
	Key            core.RateLimitKey
	Limit          int
	Remaining      int
	ResetAt        *time.Time
	RetryAfter     *time.Duration
	ThrottledUntil *time.Time
	LastStatus     int
	Attempts       int
	UpdatedAt      time.Time
	Metadata       map[string]any
}

type StateStore interface {
	Get(ctx context.Context, key core.RateLimitKey) (State, error)
	Upsert(ctx context.Context, state State) error
}

type ThrottledError struct {
	ProviderID string
	BucketKey  string
	RetryAfter time.Duration
}

func (e ThrottledError) Error() string {
	return fmt.Sprintf(
		"ratelimit: provider %q bucket %q throttled for %s",
		strings.TrimSpace(e.ProviderID),
		strings.TrimSpace(e.BucketKey),
		e.RetryAfter,
	)
}

func (e ThrottledError) ToServiceError() *goerrors.Error {
	metadata := map[string]any{
		"provider_id": strings.TrimSpace(e.ProviderID),
		"bucket_key":  strings.TrimSpace(e.BucketKey),
	}
	if e.RetryAfter > 0 {
		metadata["retry_after_ms"] = e.RetryAfter.Milliseconds()
	}
	return goerrors.New(e.Error(), goerrors.CategoryRateLimit).
		WithCode(http.StatusTooManyRequests).
		WithTextCode(core.ServiceErrorRateLimited).
		WithMetadata(metadata)
}

type AdaptivePolicy struct {
	Store            StateStore
	Now              func() time.Time
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	DefaultRetryHint time.Duration
}

func NewAdaptivePolicy(store StateStore) *AdaptivePolicy {
	return &AdaptivePolicy{
		Store:            store,
		Now:              func() time.Time { return time.Now().UTC() },
		InitialBackoff:   time.Second,
		MaxBackoff:       time.Minute,
		DefaultRetryHint: 5 * time.Second,
	}
}

func (p *AdaptivePolicy) BeforeCall(ctx context.Context, key core.RateLimitKey) error {
	if p == nil || p.Store == nil {
		return nil
	}
	state, err := p.Store.Get(ctx, normalizeKey(key))
	if err != nil {
		if errors.Is(err, ErrStateNotFound) {
			return nil
		}
		return err
	}

	now := p.now()
	if until := state.ThrottledUntil; until != nil && now.Before(*until) {
		return ThrottledError{ProviderID: state.Key.ProviderID, BucketKey: state.Key.BucketKey, RetryAfter: until.Sub(now)}
	}
	if state.Remaining == 0 && state.ResetAt != nil && now.Before(*state.ResetAt) {
		return ThrottledError{ProviderID: state.Key.ProviderID, BucketKey: state.Key.BucketKey, RetryAfter: state.ResetAt.Sub(now)}
	}
	return nil
}

func (p *AdaptivePolicy) AfterCall(ctx context.Context, key core.RateLimitKey, res core.ProviderResponseMeta) error {
	if p == nil || p.Store == nil {
		return nil
	}
	key = normalizeKey(key)
	now := p.now()
	state, err := p.Store.Get(ctx, key)
	if err != nil && !errors.Is(err, ErrStateNotFound) {
		return err
	}
	if errors.Is(err, ErrStateNotFound) {
		state = State{Key: key}
	}

	state.LastStatus = res.StatusCode
	state.UpdatedAt = now
	state.Metadata = cloneMap(state.Metadata)
	for k, v := range cloneMap(res.Metadata) {
		state.Metadata[k] = v
	}

	_, hasLimit := parseHeaderInt(res.Headers, "x-ratelimit-limit")
	if limit, ok := parseHeaderInt(res.Headers, "x-ratelimit-limit"); ok {
		state.Limit = limit
	}
	_, hasRemaining := parseHeaderInt(res.Headers, "x-ratelimit-remaining")
	if remaining, ok := parseHeaderInt(res.Headers, "x-ratelimit-remaining"); ok {
		state.Remaining = remaining
	}
	_, hasResetAt := parseHeaderResetAt(res.Headers)
	if resetAt, ok := parseHeaderResetAt(res.Headers); ok {
		state.ResetAt = &resetAt
	}

	calculatedRetryAfter, hasRetryAfter := parseRetryAfter(res, now)
	if hasRetryAfter {
		state.RetryAfter = &calculatedRetryAfter
	} else {
		state.RetryAfter = nil
	}

	if isThrottledResponse(res.StatusCode, state.Remaining, hasRemaining, hasResetAt, hasLimit, hasRetryAfter) {
		state.Attempts++
		delay := calculatedRetryAfter
		if !hasRetryAfter {
			delay = p.nextBackoff(state.Attempts)
		}
		until := now.Add(delay)
		state.ThrottledUntil = &until
		if err := p.Store.Upsert(ctx, state); err != nil {
			return err
		}
		return nil
	}

	state.Attempts = 0
	state.ThrottledUntil = nil
	if err := p.Store.Upsert(ctx, state); err != nil {
		return err
	}
	return nil
}

func (p *AdaptivePolicy) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func (p *AdaptivePolicy) nextBackoff(attempt int) time.Duration {
	initial := p.InitialBackoff
	if initial <= 0 {
		initial = time.Second
	}
	maximum := p.MaxBackoff
	if maximum <= 0 {
		maximum = time.Minute
	}
	if attempt <= 0 {
		return initial
	}
	delay := initial
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maximum {
			return maximum
		}
	}
	if delay <= 0 {
		return p.defaultRetryHint()
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func (p *AdaptivePolicy) defaultRetryHint() time.Duration {
	if p != nil && p.DefaultRetryHint > 0 {
		return p.DefaultRetryHint
	}
	return 5 * time.Second
}

func isThrottledResponse(
	statusCode int,
	remaining int,
	hasRemaining bool,
	hasResetAt bool,
	hasLimit bool,
	hasRetryAfter bool,
) bool {
	if statusCode == 429 {
		return true
	}
	if statusCode >= 500 {
		return false
	}
	return remaining == 0 && (hasRemaining || hasResetAt || hasLimit || hasRetryAfter)
}

func parseRetryAfter(res core.ProviderResponseMeta, now time.Time) (time.Duration, bool) {
	if res.RetryAfter != nil && *res.RetryAfter > 0 {
		return *res.RetryAfter, true
	}
	raw := headerValue(res.Headers, "retry-after")
	if raw == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds <= 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if retryAt, err := httpDate(raw); err == nil {
		if retryAt.After(now) {
			return retryAt.Sub(now), true
		}
	}
	return 0, false
}

func parseHeaderInt(headers map[string]string, key string) (int, bool) {
	value := headerValue(headers, key)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func parseHeaderResetAt(headers map[string]string) (time.Time, bool) {
	value := headerValue(headers, "x-ratelimit-reset")
	if value == "" {
		return time.Time{}, false
	}
	unix, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	if unix <= 0 {
		return time.Time{}, false
	}
	return time.Unix(unix, 0).UTC(), true
}

func httpDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("ratelimit: empty date")
	}
	if parsed, err := time.Parse(time.RFC1123, value); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse(time.RFC1123Z, value); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("ratelimit: invalid http date")
}

func headerValue(headers map[string]string, key string) string {
	if len(headers) == 0 {
		return ""
	}
	for existing, value := range headers {
		if strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(key)) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeKey(key core.RateLimitKey) core.RateLimitKey {
	return core.RateLimitKey{
		ProviderID: strings.TrimSpace(strings.ToLower(key.ProviderID)),
		ScopeType:  strings.TrimSpace(strings.ToLower(key.ScopeType)),
		ScopeID:    strings.TrimSpace(key.ScopeID),
		BucketKey:  strings.TrimSpace(strings.ToLower(key.BucketKey)),
	}
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

type MemoryStateStore struct {
	mu    sync.RWMutex
	items map[string]State
}

func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{items: map[string]State{}}
}

func (s *MemoryStateStore) Get(_ context.Context, key core.RateLimitKey) (State, error) {
	if s == nil {
		return State{}, fmt.Errorf("ratelimit: state store is nil")
	}
	normalized := normalizeKey(key)
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.items[stateKey(normalized)]
	if !ok {
		return State{}, ErrStateNotFound
	}
	state.Metadata = cloneMap(state.Metadata)
	return state, nil
}

func (s *MemoryStateStore) Upsert(_ context.Context, state State) error {
	if s == nil {
		return fmt.Errorf("ratelimit: state store is nil")
	}
	state.Key = normalizeKey(state.Key)
	state.Metadata = cloneMap(state.Metadata)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[stateKey(state.Key)] = state
	return nil
}

func stateKey(key core.RateLimitKey) string {
	return key.ProviderID + "|" + key.ScopeType + "|" + key.ScopeID + "|" + key.BucketKey
}

var _ core.RateLimitPolicy = (*AdaptivePolicy)(nil)
