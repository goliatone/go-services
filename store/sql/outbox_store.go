package sqlstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-services/core"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

const (
	outboxStatusPending    = "pending"
	outboxStatusProcessing = "processing"
	outboxStatusDelivered  = "delivered"
	outboxStatusFailed     = "failed"
)

type OutboxStore struct {
	db   *bun.DB
	repo repository.Repository[*lifecycleOutboxRecord]
}

func NewOutboxStore(db *bun.DB) (*OutboxStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepository[*lifecycleOutboxRecord](db, outboxHandlers())
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid outbox repository wiring: %w", err)
		}
	}
	return &OutboxStore{db: db, repo: repo}, nil
}

func (s *OutboxStore) Enqueue(ctx context.Context, event core.LifecycleEvent) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: outbox store is not configured")
	}
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("sqlstore: outbox event id is required")
	}
	if strings.TrimSpace(event.Name) == "" {
		return fmt.Errorf("sqlstore: outbox event name is required")
	}
	if strings.TrimSpace(event.ProviderID) == "" {
		return fmt.Errorf("sqlstore: outbox provider id is required")
	}
	if strings.TrimSpace(event.ScopeType) == "" || strings.TrimSpace(event.ScopeID) == "" {
		return fmt.Errorf("sqlstore: outbox scope type and scope id are required")
	}

	occurredAt := event.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	record := &lifecycleOutboxRecord{
		ID:         uuid.NewString(),
		EventID:    strings.TrimSpace(event.ID),
		EventName:  strings.TrimSpace(event.Name),
		ProviderID: strings.TrimSpace(event.ProviderID),
		ScopeType:  strings.TrimSpace(event.ScopeType),
		ScopeID:    strings.TrimSpace(event.ScopeID),
		Payload:    copyAnyMap(event.Payload),
		Metadata:   copyAnyMap(event.Metadata),
		Status:     outboxStatusPending,
		Attempts:   0,
		LastError:  "",
		OccurredAt: occurredAt,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if trimmed := strings.TrimSpace(event.ConnectionID); trimmed != "" {
		record.ConnectionID = &trimmed
	}

	_, err := s.repo.Create(ctx, record)
	return err
}

func (s *OutboxStore) ClaimBatch(ctx context.Context, limit int) ([]core.LifecycleEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqlstore: outbox store is not configured")
	}
	if limit <= 0 {
		limit = 1
	}
	now := time.Now().UTC()
	var records []lifecycleOutboxRecord
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		query := `
WITH claimed AS (
	SELECT id
	FROM service_lifecycle_outbox
	WHERE status = ?
	  AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
	ORDER BY occurred_at ASC
	LIMIT ?
)
UPDATE service_lifecycle_outbox
SET status = ?, updated_at = ?
WHERE id IN (SELECT id FROM claimed)
  AND status = ?
RETURNING
	id,
	event_id,
	event_name,
	provider_id,
	scope_type,
	scope_id,
	connection_id,
	payload,
	metadata,
	status,
	attempts,
	next_attempt_at,
	last_error,
	occurred_at,
	created_at,
	updated_at
`
		return tx.NewRaw(
			query,
			outboxStatusPending,
			now,
			limit,
			outboxStatusProcessing,
			now,
			outboxStatusPending,
		).Scan(ctx, &records)
	})
	if err != nil {
		return nil, err
	}

	events := make([]core.LifecycleEvent, 0, len(records))
	for _, record := range records {
		events = append(events, outboxRecordToEvent(record))
	}
	return events, nil
}

func (s *OutboxStore) Ack(ctx context.Context, eventID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlstore: outbox store is not configured")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return fmt.Errorf("sqlstore: event id is required")
	}
	_, err := s.db.NewUpdate().
		Model((*lifecycleOutboxRecord)(nil)).
		Set("status = ?", outboxStatusDelivered).
		Set("last_error = ?", "").
		Set("next_attempt_at = NULL").
		Set("updated_at = ?", time.Now().UTC()).
		Where("event_id = ?", eventID).
		Exec(ctx)
	return err
}

func (s *OutboxStore) Retry(ctx context.Context, eventID string, cause error, nextAttemptAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlstore: outbox store is not configured")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return fmt.Errorf("sqlstore: event id is required")
	}
	status := outboxStatusPending
	var next *time.Time
	if !nextAttemptAt.IsZero() {
		nextValue := nextAttemptAt.UTC()
		next = &nextValue
	} else {
		status = outboxStatusFailed
	}

	lastError := ""
	if cause != nil {
		lastError = strings.TrimSpace(cause.Error())
	}
	_, err := s.db.NewUpdate().
		Model((*lifecycleOutboxRecord)(nil)).
		Set("status = ?", status).
		Set("attempts = attempts + 1").
		Set("next_attempt_at = ?", next).
		Set("last_error = ?", lastError).
		Set("updated_at = ?", time.Now().UTC()).
		Where("event_id = ?", eventID).
		Exec(ctx)
	return err
}

func outboxRecordToEvent(record lifecycleOutboxRecord) core.LifecycleEvent {
	event := core.LifecycleEvent{
		ID:         record.EventID,
		Name:       record.EventName,
		ProviderID: record.ProviderID,
		ScopeType:  record.ScopeType,
		ScopeID:    record.ScopeID,
		Payload:    copyAnyMap(record.Payload),
		Metadata:   copyAnyMap(record.Metadata),
		OccurredAt: record.OccurredAt,
	}
	if record.ConnectionID != nil {
		event.ConnectionID = strings.TrimSpace(*record.ConnectionID)
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	event.Metadata[core.MetadataKeyOutboxAttempts] = record.Attempts
	return event
}

var _ core.OutboxStore = (*OutboxStore)(nil)
