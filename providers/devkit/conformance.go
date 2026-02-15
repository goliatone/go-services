package devkit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/webhooks"
)

func ValidateTransportAdapterConformance(
	ctx context.Context,
	adapter core.TransportAdapter,
	request core.TransportRequest,
) error {
	if adapter == nil {
		return fmt.Errorf("devkit: transport adapter is required")
	}
	if strings.TrimSpace(adapter.Kind()) == "" {
		return fmt.Errorf("devkit: transport adapter kind is required")
	}
	_, err := adapter.Do(ctx, request)
	return err
}

func ValidateWebhookLedgerConformance(
	ctx context.Context,
	ledger webhooks.DeliveryLedger,
	providerID string,
	deliveryID string,
) error {
	if ledger == nil {
		return fmt.Errorf("devkit: delivery ledger is required")
	}
	record, accepted, err := ledger.Claim(ctx, providerID, deliveryID, nil, time.Second)
	if err != nil {
		return err
	}
	if !accepted {
		return fmt.Errorf("devkit: first claim should be accepted")
	}

	if _, accepted, err := ledger.Claim(ctx, providerID, deliveryID, nil, time.Second); err != nil {
		return err
	} else if accepted {
		return fmt.Errorf("devkit: second claim should not be accepted while lease is active")
	}

	if err := ledger.Complete(ctx, record.ClaimID); err != nil {
		return err
	}
	loaded, err := ledger.Get(ctx, providerID, deliveryID)
	if err != nil {
		return err
	}
	if loaded.Status != webhooks.DeliveryStatusProcessed {
		return fmt.Errorf("devkit: expected processed status, got %q", loaded.Status)
	}
	return nil
}

func ValidateIdempotencyClaimStoreConformance(
	ctx context.Context,
	store core.IdempotencyClaimStore,
	key string,
) error {
	if store == nil {
		return fmt.Errorf("devkit: idempotency store is required")
	}
	claimID, accepted, err := store.Claim(ctx, key, time.Minute)
	if err != nil {
		return err
	}
	if !accepted || strings.TrimSpace(claimID) == "" {
		return fmt.Errorf("devkit: first claim should be accepted")
	}
	if _, accepted, err := store.Claim(ctx, key, time.Minute); err != nil {
		return err
	} else if accepted {
		return fmt.Errorf("devkit: second claim should not be accepted")
	}
	return store.Complete(ctx, claimID)
}
