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

type NotificationDispatchStore struct {
	repo repository.Repository[*notificationDispatchRecord]
}

func NewNotificationDispatchStore(db *bun.DB) (*NotificationDispatchStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepository[*notificationDispatchRecord](db, notificationDispatchHandlers())
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid notification dispatch repository wiring: %w", err)
		}
	}
	return &NotificationDispatchStore{repo: repo}, nil
}

func (s *NotificationDispatchStore) Seen(ctx context.Context, idempotencyKey string) (bool, error) {
	if s == nil || s.repo == nil {
		return false, fmt.Errorf("sqlstore: notification dispatch store is not configured")
	}
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return false, fmt.Errorf("sqlstore: idempotency key is required")
	}
	records, _, err := s.repo.List(ctx,
		repository.SelectBy("idempotency_key", "=", key),
		repository.SelectPaginate(1, 0),
	)
	if err != nil {
		return false, err
	}
	return len(records) > 0, nil
}

func (s *NotificationDispatchStore) Record(ctx context.Context, input core.NotificationDispatchRecord) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: notification dispatch store is not configured")
	}
	if strings.TrimSpace(input.EventID) == "" {
		return fmt.Errorf("sqlstore: event id is required")
	}
	if strings.TrimSpace(input.Projector) == "" {
		return fmt.Errorf("sqlstore: projector is required")
	}
	if strings.TrimSpace(input.DefinitionCode) == "" {
		return fmt.Errorf("sqlstore: definition code is required")
	}
	if strings.TrimSpace(input.RecipientKey) == "" {
		return fmt.Errorf("sqlstore: recipient key is required")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return fmt.Errorf("sqlstore: idempotency key is required")
	}

	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "sent"
	}
	record := &notificationDispatchRecord{
		ID:           uuid.NewString(),
		EventID:      strings.TrimSpace(input.EventID),
		Projector:    strings.TrimSpace(input.Projector),
		Definition:   strings.TrimSpace(input.DefinitionCode),
		RecipientKey: strings.TrimSpace(input.RecipientKey),
		Idempotency:  strings.TrimSpace(input.IdempotencyKey),
		Status:       status,
		Error:        strings.TrimSpace(input.Error),
		Metadata:     copyAnyMap(input.Metadata),
		CreatedAt:    time.Now().UTC(),
	}
	_, err := s.repo.Create(ctx, record)
	if err != nil && isUniqueConstraintError(err) {
		return nil
	}
	return err
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") || strings.Contains(text, "duplicate")
}

var _ core.NotificationDispatchLedger = (*NotificationDispatchStore)(nil)
