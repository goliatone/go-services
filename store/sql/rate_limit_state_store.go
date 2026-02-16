package sqlstore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/ratelimit"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

const (
	stateMetaAttempts       = "_attempts"
	stateMetaLastStatus     = "_last_status"
	stateMetaThrottledUntil = "_throttled_until"
)

type RateLimitStateStore struct {
	db   *bun.DB
	repo repository.Repository[*rateLimitStateRecord]
}

func NewRateLimitStateStore(db *bun.DB) (*RateLimitStateStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepositoryWithConfig[*rateLimitStateRecord](
		db,
		rateLimitStateHandlers(),
		nil,
		repository.WithRecordLookupResolver(rateLimitStateLookupResolver),
	)
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid rate-limit state repository wiring: %w", err)
		}
	}
	return &RateLimitStateStore{
		db:   db,
		repo: repo,
	}, nil
}

func (s *RateLimitStateStore) Get(ctx context.Context, key core.RateLimitKey) (ratelimit.State, error) {
	if s == nil || s.repo == nil {
		return ratelimit.State{}, fmt.Errorf("sqlstore: rate-limit state store is not configured")
	}
	key = normalizeRateLimitKey(key)
	if err := validateRateLimitKey(key); err != nil {
		return ratelimit.State{}, err
	}

	record, err := s.repo.Get(ctx,
		repository.SelectBy("provider_id", "=", key.ProviderID),
		repository.SelectBy("scope_type", "=", key.ScopeType),
		repository.SelectBy("scope_id", "=", key.ScopeID),
		repository.SelectBy("bucket_key", "=", key.BucketKey),
		repository.SelectOrderDesc("updated_at"),
		repository.SelectOrderAsc("id"),
	)
	if err != nil {
		if repository.IsRecordNotFound(err) {
			return ratelimit.State{}, ratelimit.ErrStateNotFound
		}
		return ratelimit.State{}, err
	}
	if record == nil {
		return ratelimit.State{}, ratelimit.ErrStateNotFound
	}
	return record.toDomain(), nil
}

func (s *RateLimitStateStore) Upsert(ctx context.Context, state ratelimit.State) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: rate-limit state store is not configured")
	}
	state.Key = normalizeRateLimitKey(state.Key)
	if err := validateRateLimitKey(state.Key); err != nil {
		return err
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	state.Metadata = copyAnyMap(state.Metadata)
	record := rateLimitStateFromDomain(state)

	_, err := s.repo.Upsert(ctx, record, repository.UpdateExcludeColumns("created_at"))
	if err == nil {
		return nil
	}
	if !repository.IsDuplicatedKey(err) {
		return err
	}

	_, retryErr := s.repo.Upsert(
		ctx,
		rateLimitStateFromDomain(state),
		repository.UpdateExcludeColumns("created_at"),
	)
	return retryErr
}

func (r *rateLimitStateRecord) toDomain() ratelimit.State {
	if r == nil {
		return ratelimit.State{}
	}
	state := ratelimit.State{
		Key: core.RateLimitKey{
			ProviderID: r.ProviderID,
			ScopeType:  r.ScopeType,
			ScopeID:    r.ScopeID,
			BucketKey:  r.BucketKey,
		},
		Limit:     r.Limit,
		Remaining: r.Remaining,
		UpdatedAt: r.UpdatedAt,
		Metadata:  copyAnyMap(r.Metadata),
	}
	if r.ResetAt != nil {
		value := *r.ResetAt
		state.ResetAt = &value
	}
	if r.RetryAfter != nil && *r.RetryAfter > 0 {
		value := time.Duration(*r.RetryAfter) * time.Second
		state.RetryAfter = &value
	}

	if attempts, ok := readIntMetadata(state.Metadata, stateMetaAttempts); ok {
		state.Attempts = attempts
		delete(state.Metadata, stateMetaAttempts)
	}
	if status, ok := readIntMetadata(state.Metadata, stateMetaLastStatus); ok {
		state.LastStatus = status
		delete(state.Metadata, stateMetaLastStatus)
	}
	if raw, ok := state.Metadata[stateMetaThrottledUntil]; ok {
		if parsed, ok := parseTimeMetadata(raw); ok {
			state.ThrottledUntil = &parsed
		}
		delete(state.Metadata, stateMetaThrottledUntil)
	}

	return state
}

func rateLimitStateLookupResolver(record *rateLimitStateRecord) []repository.SelectCriteria {
	if record == nil {
		return nil
	}
	return []repository.SelectCriteria{
		repository.SelectBy("provider_id", "=", strings.TrimSpace(strings.ToLower(record.ProviderID))),
		repository.SelectBy("scope_type", "=", strings.TrimSpace(strings.ToLower(record.ScopeType))),
		repository.SelectBy("scope_id", "=", strings.TrimSpace(record.ScopeID)),
		repository.SelectBy("bucket_key", "=", strings.TrimSpace(strings.ToLower(record.BucketKey))),
		repository.SelectOrderDesc("updated_at"),
	}
}

func rateLimitStateFromDomain(state ratelimit.State) *rateLimitStateRecord {
	updatedAt := state.UpdatedAt.UTC()
	return &rateLimitStateRecord{
		ID:         uuid.NewString(),
		ProviderID: state.Key.ProviderID,
		ScopeType:  state.Key.ScopeType,
		ScopeID:    state.Key.ScopeID,
		BucketKey:  state.Key.BucketKey,
		Limit:      state.Limit,
		Remaining:  state.Remaining,
		ResetAt:    copyTimePointer(state.ResetAt),
		RetryAfter: durationToSecondsPointer(state.RetryAfter),
		Metadata:   composeRateLimitMetadata(state),
		CreatedAt:  updatedAt,
		UpdatedAt:  updatedAt,
	}
}

func composeRateLimitMetadata(state ratelimit.State) map[string]any {
	metadata := copyAnyMap(state.Metadata)
	if state.Attempts > 0 {
		metadata[stateMetaAttempts] = state.Attempts
	} else {
		delete(metadata, stateMetaAttempts)
	}
	if state.LastStatus > 0 {
		metadata[stateMetaLastStatus] = state.LastStatus
	} else {
		delete(metadata, stateMetaLastStatus)
	}
	if state.ThrottledUntil != nil {
		metadata[stateMetaThrottledUntil] = state.ThrottledUntil.UTC().Format(time.RFC3339Nano)
	} else {
		delete(metadata, stateMetaThrottledUntil)
	}
	return metadata
}

func normalizeRateLimitKey(key core.RateLimitKey) core.RateLimitKey {
	return core.RateLimitKey{
		ProviderID: strings.TrimSpace(strings.ToLower(key.ProviderID)),
		ScopeType:  strings.TrimSpace(strings.ToLower(key.ScopeType)),
		ScopeID:    strings.TrimSpace(key.ScopeID),
		BucketKey:  strings.TrimSpace(strings.ToLower(key.BucketKey)),
	}
}

func validateRateLimitKey(key core.RateLimitKey) error {
	if strings.TrimSpace(key.ProviderID) == "" {
		return fmt.Errorf("sqlstore: rate-limit provider id is required")
	}
	if strings.TrimSpace(key.ScopeType) == "" {
		return fmt.Errorf("sqlstore: rate-limit scope type is required")
	}
	if strings.TrimSpace(key.ScopeID) == "" {
		return fmt.Errorf("sqlstore: rate-limit scope id is required")
	}
	if strings.TrimSpace(key.BucketKey) == "" {
		return fmt.Errorf("sqlstore: rate-limit bucket key is required")
	}
	return nil
}

func copyTimePointer(input *time.Time) *time.Time {
	if input == nil {
		return nil
	}
	value := input.UTC()
	return &value
}

func durationToSecondsPointer(input *time.Duration) *int {
	if input == nil || *input <= 0 {
		return nil
	}
	seconds := int(input.Seconds())
	if seconds <= 0 {
		seconds = 1
	}
	return &seconds
}

func parseTimeMetadata(input any) (time.Time, bool) {
	switch typed := input.(type) {
	case time.Time:
		return typed.UTC(), true
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(typed))
		if err != nil {
			return time.Time{}, false
		}
		return parsed.UTC(), true
	default:
		return time.Time{}, false
	}
}

func readIntMetadata(metadata map[string]any, key string) (int, bool) {
	raw, ok := metadata[key]
	if !ok {
		return 0, false
	}
	switch typed := raw.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

var _ ratelimit.StateStore = (*RateLimitStateStore)(nil)
