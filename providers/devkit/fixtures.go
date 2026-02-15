package devkit

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/inbound"
	"github.com/goliatone/go-services/webhooks"
)

func NewIdempotencyClaimStoreFixture() core.IdempotencyClaimStore {
	return inbound.NewInMemoryClaimStore()
}

type WebhookDeliveryLedgerFixture struct {
	mu      sync.Mutex
	records map[string]webhooks.DeliveryRecord
	now     func() time.Time
}

func NewWebhookDeliveryLedgerFixture() *WebhookDeliveryLedgerFixture {
	return &WebhookDeliveryLedgerFixture{
		records: map[string]webhooks.DeliveryRecord{},
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (l *WebhookDeliveryLedgerFixture) Claim(
	_ context.Context,
	providerID string,
	deliveryID string,
	_ []byte,
	lease time.Duration,
) (webhooks.DeliveryRecord, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := ledgerKey(providerID, deliveryID)
	now := l.currentTime()
	if lease <= 0 {
		lease = 30 * time.Second
	}

	record, ok := l.records[key]
	if !ok {
		record = webhooks.DeliveryRecord{
			ID:         key,
			ProviderID: strings.TrimSpace(providerID),
			DeliveryID: strings.TrimSpace(deliveryID),
			Status:     webhooks.DeliveryStatusPending,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	}
	switch record.Status {
	case webhooks.DeliveryStatusProcessed, webhooks.DeliveryStatusDead:
		l.records[key] = record
		return record, false, nil
	case webhooks.DeliveryStatusRetryReady, webhooks.DeliveryStatusProcessing:
		if record.NextAttemptAt != nil && now.Before(record.NextAttemptAt.UTC()) {
			l.records[key] = record
			return record, false, nil
		}
	}

	record.Status = webhooks.DeliveryStatusProcessing
	record.Attempts++
	record.ClaimID = key + ":" + strconv.Itoa(record.Attempts)
	next := now.Add(lease)
	record.NextAttemptAt = &next
	record.UpdatedAt = now
	l.records[key] = record
	return record, true, nil
}

func (l *WebhookDeliveryLedgerFixture) Get(
	_ context.Context,
	providerID string,
	deliveryID string,
) (webhooks.DeliveryRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	record, ok := l.records[ledgerKey(providerID, deliveryID)]
	if !ok {
		return webhooks.DeliveryRecord{}, fmt.Errorf("devkit: delivery not found")
	}
	return record, nil
}

func (l *WebhookDeliveryLedgerFixture) Complete(_ context.Context, claimID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	key, attempt, err := parseLedgerClaimID(claimID)
	if err != nil {
		return err
	}
	record, ok := l.records[key]
	if !ok {
		return fmt.Errorf("devkit: delivery not found")
	}
	if record.Status != webhooks.DeliveryStatusProcessing || record.Attempts != attempt {
		return nil
	}
	record.Status = webhooks.DeliveryStatusProcessed
	record.NextAttemptAt = nil
	record.UpdatedAt = l.currentTime()
	l.records[key] = record
	return nil
}

func (l *WebhookDeliveryLedgerFixture) Fail(
	_ context.Context,
	claimID string,
	_ error,
	nextAttemptAt time.Time,
	maxAttempts int,
) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	key, attempt, err := parseLedgerClaimID(claimID)
	if err != nil {
		return err
	}
	record, ok := l.records[key]
	if !ok {
		return fmt.Errorf("devkit: delivery not found")
	}
	if record.Status != webhooks.DeliveryStatusProcessing || record.Attempts != attempt {
		return nil
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	if record.Attempts >= maxAttempts {
		record.Status = webhooks.DeliveryStatusDead
		record.NextAttemptAt = nil
	} else {
		record.Status = webhooks.DeliveryStatusRetryReady
		if nextAttemptAt.IsZero() {
			nextAttemptAt = l.currentTime()
		}
		record.NextAttemptAt = &nextAttemptAt
	}
	record.UpdatedAt = l.currentTime()
	l.records[key] = record
	return nil
}

func (l *WebhookDeliveryLedgerFixture) Snapshot() []webhooks.DeliveryRecord {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make([]webhooks.DeliveryRecord, 0, len(l.records))
	for _, record := range l.records {
		cloned := record
		if record.NextAttemptAt != nil {
			next := *record.NextAttemptAt
			cloned.NextAttemptAt = &next
		}
		out = append(out, cloned)
	}
	return out
}

func (l *WebhookDeliveryLedgerFixture) currentTime() time.Time {
	if l != nil && l.now != nil {
		return l.now().UTC()
	}
	return time.Now().UTC()
}

func ledgerKey(providerID string, deliveryID string) string {
	return strings.TrimSpace(providerID) + ":" + strings.TrimSpace(deliveryID)
}

func parseLedgerClaimID(claimID string) (string, int, error) {
	parts := strings.Split(strings.TrimSpace(claimID), ":")
	if len(parts) < 3 {
		return "", 0, fmt.Errorf("devkit: invalid claim id")
	}
	attempt, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || attempt <= 0 {
		return "", 0, fmt.Errorf("devkit: invalid claim id")
	}
	key := strings.Join(parts[:len(parts)-1], ":")
	return key, attempt, nil
}

var _ webhooks.DeliveryLedger = (*WebhookDeliveryLedgerFixture)(nil)
