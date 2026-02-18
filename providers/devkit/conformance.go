package devkit

import (
	"context"
	"fmt"
	"net/http"
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

func ValidateSigV4SigningConformance(
	ctx context.Context,
	signer core.Signer,
	fixture SigV4Fixture,
) error {
	if signer == nil {
		return fmt.Errorf("devkit: signer is required")
	}
	if fixture.Request == nil {
		return fmt.Errorf("devkit: request fixture is required")
	}
	if err := signer.Sign(ctx, fixture.Request, fixture.Credential); err != nil {
		return err
	}

	mode := strings.TrimSpace(strings.ToLower(fixture.ExpectMode))
	switch mode {
	case "query":
		query := fixture.Request.URL.Query()
		if strings.TrimSpace(query.Get("X-Amz-Signature")) == "" {
			return fmt.Errorf("devkit: expected sigv4 query signature")
		}
		if strings.TrimSpace(query.Get("X-Amz-Credential")) == "" {
			return fmt.Errorf("devkit: expected sigv4 query credential")
		}
	default:
		authHeader := strings.TrimSpace(fixture.Request.Header.Get("Authorization"))
		if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
			return fmt.Errorf("devkit: expected sigv4 authorization header")
		}
		if strings.TrimSpace(fixture.Request.Header.Get("X-Amz-Date")) == "" {
			return fmt.Errorf("devkit: expected x-amz-date header")
		}
	}

	host := strings.TrimSpace(fixture.Request.URL.Host)
	if host == "" {
		return fmt.Errorf("devkit: signed host must not be empty")
	}
	return nil
}

func ValidateWebhookTemplateConformance(
	ctx context.Context,
	fixture WebhookTemplateFixture,
) error {
	if fixture.Template.Verifier == nil {
		return fmt.Errorf("devkit: template verifier is required")
	}
	if fixture.Template.Extractor == nil {
		return fmt.Errorf("devkit: template extractor is required")
	}
	if err := fixture.Template.Verifier.Verify(ctx, fixture.Request); err != nil {
		return err
	}
	deliveryID, err := fixture.Template.Extractor(fixture.Request)
	if err != nil {
		return err
	}
	if strings.TrimSpace(deliveryID) == "" {
		return fmt.Errorf("devkit: extracted delivery id is required")
	}
	if expected := strings.TrimSpace(fixture.DeliveryID); expected != "" && deliveryID != expected {
		return fmt.Errorf("devkit: unexpected delivery id %q (want %q)", deliveryID, expected)
	}
	return nil
}

func ValidateSigV4ReplayWindow(
	_ context.Context,
	request *http.Request,
	maxWindow time.Duration,
	now time.Time,
) error {
	if request == nil {
		return fmt.Errorf("devkit: request is required")
	}
	if maxWindow <= 0 {
		maxWindow = 5 * time.Minute
	}
	rawSignedAt := strings.TrimSpace(request.Header.Get("X-Amz-Date"))
	if rawSignedAt == "" {
		rawSignedAt = strings.TrimSpace(request.URL.Query().Get("X-Amz-Date"))
	}
	if rawSignedAt == "" {
		return fmt.Errorf("devkit: x-amz-date is required")
	}
	signedAt, err := time.Parse("20060102T150405Z", rawSignedAt)
	if err != nil {
		return err
	}
	diff := now.Sub(signedAt)
	if diff < 0 {
		diff = -diff
	}
	if diff > maxWindow {
		return fmt.Errorf("devkit: sigv4 signed request is outside replay window")
	}
	return nil
}
