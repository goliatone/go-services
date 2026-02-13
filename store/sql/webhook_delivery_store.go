package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-services/webhooks"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type WebhookDeliveryStore struct {
	db   *bun.DB
	repo repository.Repository[*webhookDeliveryRecord]
}

func NewWebhookDeliveryStore(db *bun.DB) (*WebhookDeliveryStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepository[*webhookDeliveryRecord](db, webhookDeliveryHandlers())
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid webhook delivery repository wiring: %w", err)
		}
	}
	return &WebhookDeliveryStore{
		db:   db,
		repo: repo,
	}, nil
}

func (s *WebhookDeliveryStore) Reserve(
	ctx context.Context,
	providerID string,
	deliveryID string,
	payload []byte,
) (webhooks.DeliveryRecord, bool, error) {
	if s == nil || s.db == nil {
		return webhooks.DeliveryRecord{}, false, fmt.Errorf("sqlstore: webhook delivery store is not configured")
	}
	providerID = strings.TrimSpace(providerID)
	deliveryID = strings.TrimSpace(deliveryID)
	if providerID == "" || deliveryID == "" {
		return webhooks.DeliveryRecord{}, false, fmt.Errorf("sqlstore: provider id and delivery id are required")
	}

	record := &webhookDeliveryRecord{
		ID:         uuid.NewString(),
		ProviderID: providerID,
		DeliveryID: deliveryID,
		Status:     webhooks.DeliveryStatusPending,
		Attempts:   1,
		Payload:    append([]byte(nil), payload...),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if _, err := s.db.NewInsert().Model(record).Exec(ctx); err != nil {
		if isUniqueViolation(err) {
			existing, getErr := s.Get(ctx, providerID, deliveryID)
			if getErr != nil {
				return webhooks.DeliveryRecord{}, false, getErr
			}
			return existing, true, nil
		}
		return webhooks.DeliveryRecord{}, false, err
	}
	return webhookDeliveryToDomain(record), false, nil
}

func (s *WebhookDeliveryStore) Get(
	ctx context.Context,
	providerID string,
	deliveryID string,
) (webhooks.DeliveryRecord, error) {
	if s == nil || s.db == nil {
		return webhooks.DeliveryRecord{}, fmt.Errorf("sqlstore: webhook delivery store is not configured")
	}
	record := &webhookDeliveryRecord{}
	err := s.db.NewSelect().
		Model(record).
		Where("?TableAlias.provider_id = ?", strings.TrimSpace(providerID)).
		Where("?TableAlias.delivery_id = ?", strings.TrimSpace(deliveryID)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return webhooks.DeliveryRecord{}, fmt.Errorf(
				"sqlstore: webhook delivery not found for provider %q delivery %q",
				providerID,
				deliveryID,
			)
		}
		return webhooks.DeliveryRecord{}, err
	}
	return webhookDeliveryToDomain(record), nil
}

func (s *WebhookDeliveryStore) MarkProcessed(
	ctx context.Context,
	providerID string,
	deliveryID string,
) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlstore: webhook delivery store is not configured")
	}
	now := time.Now().UTC()
	_, err := s.db.NewUpdate().
		Model((*webhookDeliveryRecord)(nil)).
		Set("status = ?", webhooks.DeliveryStatusProcessed).
		Set("next_attempt_at = NULL").
		Set("updated_at = ?", now).
		Where("provider_id = ?", strings.TrimSpace(providerID)).
		Where("delivery_id = ?", strings.TrimSpace(deliveryID)).
		Exec(ctx)
	return err
}

func (s *WebhookDeliveryStore) MarkRetry(
	ctx context.Context,
	providerID string,
	deliveryID string,
	_ error,
	nextAttemptAt time.Time,
) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlstore: webhook delivery store is not configured")
	}
	record, err := s.Get(ctx, providerID, deliveryID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = s.db.NewUpdate().
		Model((*webhookDeliveryRecord)(nil)).
		Set("status = ?", webhooks.DeliveryStatusRetryReady).
		Set("attempts = ?", record.Attempts+1).
		Set("next_attempt_at = ?", nextAttemptAt.UTC()).
		Set("updated_at = ?", now).
		Where("provider_id = ?", strings.TrimSpace(providerID)).
		Where("delivery_id = ?", strings.TrimSpace(deliveryID)).
		Exec(ctx)
	return err
}

func webhookDeliveryToDomain(record *webhookDeliveryRecord) webhooks.DeliveryRecord {
	if record == nil {
		return webhooks.DeliveryRecord{}
	}
	result := webhooks.DeliveryRecord{
		ID:         record.ID,
		ProviderID: record.ProviderID,
		DeliveryID: record.DeliveryID,
		Status:     record.Status,
		Attempts:   record.Attempts,
		CreatedAt:  record.CreatedAt,
		UpdatedAt:  record.UpdatedAt,
	}
	if record.NextAttemptAt != nil {
		value := *record.NextAttemptAt
		result.NextAttemptAt = &value
	}
	return result
}

func isUniqueViolation(err error) bool {
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key value violates unique constraint")
}

var _ webhooks.DeliveryLedger = (*WebhookDeliveryStore)(nil)
