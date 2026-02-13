package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
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

func (s *WebhookDeliveryStore) Claim(
	ctx context.Context,
	providerID string,
	deliveryID string,
	payload []byte,
	lease time.Duration,
) (webhooks.DeliveryRecord, bool, error) {
	if s == nil || s.db == nil {
		return webhooks.DeliveryRecord{}, false, fmt.Errorf("sqlstore: webhook delivery store is not configured")
	}
	providerID = strings.TrimSpace(providerID)
	deliveryID = strings.TrimSpace(deliveryID)
	if providerID == "" || deliveryID == "" {
		return webhooks.DeliveryRecord{}, false, fmt.Errorf("sqlstore: provider id and delivery id are required")
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	now := time.Now().UTC()

	record := &webhookDeliveryRecord{
		ID:         uuid.NewString(),
		ProviderID: providerID,
		DeliveryID: deliveryID,
		Status:     webhooks.DeliveryStatusPending,
		Attempts:   0,
		Payload:    append([]byte(nil), payload...),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if _, err := s.db.NewInsert().
		Model(record).
		On("CONFLICT (provider_id, delivery_id) DO NOTHING").
		Exec(ctx); err != nil {
		return webhooks.DeliveryRecord{}, false, err
	}

	leaseUntil := now.Add(lease).UTC()
	result, err := s.db.NewUpdate().
		Model((*webhookDeliveryRecord)(nil)).
		Set("status = ?", webhooks.DeliveryStatusProcessing).
		Set("attempts = CASE WHEN attempts < 1 THEN 1 ELSE attempts + 1 END").
		Set("next_attempt_at = ?", leaseUntil).
		Set("updated_at = ?", now).
		Where("provider_id = ?", providerID).
		Where("delivery_id = ?", deliveryID).
		Where(
			"(status = ? OR (status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)))",
			webhooks.DeliveryStatusPending,
			webhooks.DeliveryStatusRetryReady,
			now,
			webhooks.DeliveryStatusProcessing,
			now,
		).
		Exec(ctx)
	if err != nil {
		return webhooks.DeliveryRecord{}, false, err
	}

	rowsAffected, _ := result.RowsAffected()
	claimed, getErr := s.Get(ctx, providerID, deliveryID)
	if getErr != nil {
		return webhooks.DeliveryRecord{}, false, getErr
	}
	if rowsAffected == 0 {
		return claimed, false, nil
	}

	claimed.ClaimID = buildDeliveryClaimID(providerID, deliveryID, claimed.Attempts)
	return claimed, true, nil
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

func (s *WebhookDeliveryStore) Complete(ctx context.Context, claimID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlstore: webhook delivery store is not configured")
	}
	providerID, deliveryID, attempt, err := parseDeliveryClaimID(claimID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := s.db.NewUpdate().
		Model((*webhookDeliveryRecord)(nil)).
		Set("status = ?", webhooks.DeliveryStatusProcessed).
		Set("next_attempt_at = NULL").
		Set("updated_at = ?", now).
		Where("provider_id = ?", providerID).
		Where("delivery_id = ?", deliveryID).
		Where("status = ?", webhooks.DeliveryStatusProcessing).
		Where("attempts = ?", attempt).
		Exec(ctx)
	if err != nil {
		return err
	}
	_, _ = result.RowsAffected()
	return nil
}

func (s *WebhookDeliveryStore) Fail(
	ctx context.Context,
	claimID string,
	_ error,
	nextAttemptAt time.Time,
	maxAttempts int,
) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlstore: webhook delivery store is not configured")
	}
	providerID, deliveryID, attempt, err := parseDeliveryClaimID(claimID)
	if err != nil {
		return err
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	record, err := s.Get(ctx, providerID, deliveryID)
	if err != nil {
		return err
	}
	if record.Status != webhooks.DeliveryStatusProcessing || record.Attempts != attempt {
		return nil
	}

	status := webhooks.DeliveryStatusRetryReady
	shouldSetNextAttempt := true
	if record.Attempts >= maxAttempts {
		status = webhooks.DeliveryStatusDead
		shouldSetNextAttempt = false
	}
	now := time.Now().UTC()
	query := s.db.NewUpdate().
		Model((*webhookDeliveryRecord)(nil)).
		Set("status = ?", status).
		Set("updated_at = ?", now).
		Where("provider_id = ?", providerID).
		Where("delivery_id = ?", deliveryID).
		Where("status = ?", webhooks.DeliveryStatusProcessing).
		Where("attempts = ?", attempt)

	if shouldSetNextAttempt {
		if nextAttemptAt.IsZero() {
			nextAttemptAt = now
		}
		query = query.Set("next_attempt_at = ?", nextAttemptAt.UTC())
	} else {
		query = query.Set("next_attempt_at = NULL")
	}

	_, err = query.Exec(ctx)
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

func buildDeliveryClaimID(providerID, deliveryID string, attempt int) string {
	return fmt.Sprintf(
		"%s|%s|%d",
		url.QueryEscape(strings.TrimSpace(providerID)),
		url.QueryEscape(strings.TrimSpace(deliveryID)),
		attempt,
	)
}

func parseDeliveryClaimID(claimID string) (string, string, int, error) {
	parts := strings.Split(strings.TrimSpace(claimID), "|")
	if len(parts) != 3 {
		return "", "", 0, fmt.Errorf("sqlstore: invalid delivery claim id")
	}
	providerID, err := url.QueryUnescape(parts[0])
	if err != nil {
		return "", "", 0, fmt.Errorf("sqlstore: invalid delivery claim id: %w", err)
	}
	deliveryID, err := url.QueryUnescape(parts[1])
	if err != nil {
		return "", "", 0, fmt.Errorf("sqlstore: invalid delivery claim id: %w", err)
	}
	attempt, err := strconv.Atoi(parts[2])
	if err != nil || attempt <= 0 {
		return "", "", 0, fmt.Errorf("sqlstore: invalid delivery claim id")
	}
	return strings.TrimSpace(providerID), strings.TrimSpace(deliveryID), attempt, nil
}

func isUniqueViolation(err error) bool {
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key value violates unique constraint")
}

var _ webhooks.DeliveryLedger = (*WebhookDeliveryStore)(nil)
