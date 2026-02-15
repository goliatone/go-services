package sqlstore

import (
	"context"
	"database/sql"
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
	repo := repository.NewRepository[*rateLimitStateRecord](db, rateLimitStateHandlers())
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
	if s == nil || s.db == nil {
		return ratelimit.State{}, fmt.Errorf("sqlstore: rate-limit state store is not configured")
	}
	key = normalizeRateLimitKey(key)
	if err := validateRateLimitKey(key); err != nil {
		return ratelimit.State{}, err
	}

	record := &rateLimitStateRecord{}
	err := s.db.NewSelect().
		Model(record).
		Where("?TableAlias.provider_id = ?", key.ProviderID).
		Where("?TableAlias.scope_type = ?", key.ScopeType).
		Where("?TableAlias.scope_id = ?", key.ScopeID).
		Where("?TableAlias.bucket_key = ?", key.BucketKey).
		OrderExpr("?TableAlias.updated_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return ratelimit.State{}, ratelimit.ErrStateNotFound
		}
		return ratelimit.State{}, err
	}
	return record.toDomain(), nil
}

func (s *RateLimitStateStore) Upsert(ctx context.Context, state ratelimit.State) error {
	if s == nil || s.db == nil {
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

	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		record, err := findRateLimitStateTx(ctx, tx, state.Key)
		if err != nil {
			return err
		}
		created := false
		if record == nil {
			created = true
			record = &rateLimitStateRecord{
				ID:         uuid.NewString(),
				ProviderID: state.Key.ProviderID,
				ScopeType:  state.Key.ScopeType,
				ScopeID:    state.Key.ScopeID,
				BucketKey:  state.Key.BucketKey,
				CreatedAt:  state.UpdatedAt,
			}
		}
		record.ProviderID = state.Key.ProviderID
		record.ScopeType = state.Key.ScopeType
		record.ScopeID = state.Key.ScopeID
		record.BucketKey = state.Key.BucketKey
		record.Limit = state.Limit
		record.Remaining = state.Remaining
		record.Metadata = composeRateLimitMetadata(state)
		record.UpdatedAt = state.UpdatedAt.UTC()
		record.ResetAt = copyTimePointer(state.ResetAt)
		record.RetryAfter = durationToSecondsPointer(state.RetryAfter)

		if created {
			if _, insertErr := tx.NewInsert().Model(record).Exec(ctx); insertErr != nil {
				return insertErr
			}
			return nil
		}
		if _, updateErr := tx.NewUpdate().
			Model(record).
			Where("id = ?", record.ID).
			Exec(ctx); updateErr != nil {
			return updateErr
		}
		return nil
	})
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

func findRateLimitStateTx(
	ctx context.Context,
	tx bun.Tx,
	key core.RateLimitKey,
) (*rateLimitStateRecord, error) {
	record := &rateLimitStateRecord{}
	err := tx.NewSelect().
		Model(record).
		Where("?TableAlias.provider_id = ?", key.ProviderID).
		Where("?TableAlias.scope_type = ?", key.ScopeType).
		Where("?TableAlias.scope_id = ?", key.ScopeID).
		Where("?TableAlias.bucket_key = ?", key.BucketKey).
		OrderExpr("?TableAlias.updated_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
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
