package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const (
	DeliveryStatusPending    = "pending"
	DeliveryStatusProcessing = "processing"
	DeliveryStatusProcessed  = "processed"
	DeliveryStatusRetryReady = "retry_ready"
	DeliveryStatusDead       = "dead"
)

type DeliveryRecord struct {
	ID            string
	ClaimID       string
	ProviderID    string
	DeliveryID    string
	Status        string
	Attempts      int
	NextAttemptAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type DeliveryLedger interface {
	Claim(
		ctx context.Context,
		providerID string,
		deliveryID string,
		payload []byte,
		lease time.Duration,
	) (DeliveryRecord, bool, error)
	Get(ctx context.Context, providerID string, deliveryID string) (DeliveryRecord, error)
	Complete(ctx context.Context, claimID string) error
	Fail(ctx context.Context, claimID string, cause error, nextAttemptAt time.Time, maxAttempts int) error
}

type Verifier interface {
	Verify(ctx context.Context, req core.InboundRequest) error
}

type DeliveryIDExtractor func(req core.InboundRequest) (string, error)

type RetryPolicy interface {
	NextDelay(attempt int) time.Duration
}

type Handler interface {
	Handle(ctx context.Context, req core.InboundRequest) (core.InboundResult, error)
}

type ExponentialRetryPolicy struct {
	Initial time.Duration
	Max     time.Duration
}

func (p ExponentialRetryPolicy) NextDelay(attempt int) time.Duration {
	initial := p.Initial
	if initial <= 0 {
		initial = time.Second
	}
	maximum := p.Max
	if maximum <= 0 {
		maximum = 30 * time.Second
	}
	delay := initial
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maximum {
			return maximum
		}
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

type Processor struct {
	Verifier    Verifier
	Ledger      DeliveryLedger
	Handler     Handler
	ExtractID   DeliveryIDExtractor
	Burst       BurstController
	RetryPolicy RetryPolicy
	// AllowAcceptedServerErrors changes default retry behavior for accepted 5xx handler responses.
	// Default (false): accepted 5xx responses are treated as retryable errors.
	AllowAcceptedServerErrors bool
	ClaimLease                time.Duration
	MaxAttempts               int
	Now                       func() time.Time
}

func NewProcessor(verifier Verifier, ledger DeliveryLedger, handler Handler) *Processor {
	return &Processor{
		Verifier:                  verifier,
		Ledger:                    ledger,
		Handler:                   handler,
		ExtractID:                 DefaultDeliveryIDExtractor,
		RetryPolicy:               ExponentialRetryPolicy{},
		AllowAcceptedServerErrors: false,
		ClaimLease:                30 * time.Second,
		MaxAttempts:               8,
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (p *Processor) Process(ctx context.Context, req core.InboundRequest) (core.InboundResult, error) {
	if p == nil || p.Handler == nil || p.Ledger == nil {
		return core.InboundResult{}, fmt.Errorf("webhooks: processor requires handler and ledger")
	}

	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return core.InboundResult{}, fmt.Errorf("webhooks: provider id is required")
	}
	req.ProviderID = providerID

	if p.Verifier != nil {
		if err := p.Verifier.Verify(ctx, req); err != nil {
			return core.InboundResult{
				Accepted:   false,
				StatusCode: http.StatusUnauthorized,
				Metadata: map[string]any{
					"provider_id": providerID,
					"rejected":    true,
				},
			}, err
		}
	}

	extractor := p.ExtractID
	if extractor == nil {
		extractor = DefaultDeliveryIDExtractor
	}
	deliveryID, err := extractor(req)
	if err != nil {
		return core.InboundResult{}, err
	}

	delivery, claimed, err := p.Ledger.Claim(ctx, providerID, deliveryID, req.Body, p.claimLease())
	if err != nil {
		return core.InboundResult{}, err
	}
	if !claimed {
		return core.InboundResult{
			Accepted:   true,
			StatusCode: http.StatusOK,
			Metadata: map[string]any{
				"provider_id": providerID,
				"delivery_id": delivery.DeliveryID,
				"status":      delivery.Status,
				"deduped":     true,
			},
		}, nil
	}

	if p.Burst != nil {
		decision, burstErr := p.Burst.Allow(ctx, req)
		if burstErr != nil {
			return core.InboundResult{}, burstErr
		}
		if !decision.Allow {
			if markErr := p.Ledger.Complete(ctx, delivery.ClaimID); markErr != nil {
				return core.InboundResult{}, markErr
			}
			metadata := ensureMetadata(decision.Metadata)
			metadata["provider_id"] = providerID
			metadata["delivery_id"] = deliveryID
			metadata["deduped"] = true
			return core.InboundResult{
				Accepted:   true,
				StatusCode: http.StatusOK,
				Metadata:   metadata,
			}, nil
		}
	}

	result, err := p.Handler.Handle(ctx, req)
	if err != nil {
		nextAttemptAt := p.now().Add(p.retryPolicy().NextDelay(delivery.Attempts))
		_ = p.Ledger.Fail(ctx, delivery.ClaimID, err, nextAttemptAt, p.maxAttempts())
		return core.InboundResult{}, err
	}

	retryableServerFailure := result.StatusCode >= http.StatusInternalServerError &&
		(!result.Accepted || !p.AllowAcceptedServerErrors)
	if !result.Accepted || retryableServerFailure {
		retryErr := fmt.Errorf("webhooks: delivery handler returned retryable status %d", result.StatusCode)
		nextAttemptAt := p.now().Add(p.retryPolicy().NextDelay(delivery.Attempts))
		_ = p.Ledger.Fail(ctx, delivery.ClaimID, retryErr, nextAttemptAt, p.maxAttempts())
		if !result.Accepted || retryableServerFailure {
			return result, retryErr
		}
	}

	if err := p.Ledger.Complete(ctx, delivery.ClaimID); err != nil {
		return core.InboundResult{}, err
	}
	result.Metadata = ensureMetadata(result.Metadata)
	result.Metadata["provider_id"] = providerID
	result.Metadata["delivery_id"] = deliveryID
	return result, nil
}

func DefaultDeliveryIDExtractor(req core.InboundRequest) (string, error) {
	if req.Metadata != nil {
		if value := strings.TrimSpace(fmt.Sprint(req.Metadata["delivery_id"])); value != "" && value != "<nil>" {
			return value, nil
		}
		if value := strings.TrimSpace(fmt.Sprint(req.Metadata["message_id"])); value != "" && value != "<nil>" {
			return value, nil
		}
	}
	if req.Headers != nil {
		if value := headerValue(req.Headers, "x-delivery-id"); value != "" {
			return value, nil
		}
		if value := headerValue(req.Headers, "x-github-delivery"); value != "" {
			return value, nil
		}
		if value := headerValue(req.Headers, "x-goog-message-number"); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("webhooks: delivery id is required for dedupe")
}

func (p *Processor) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func (p *Processor) retryPolicy() RetryPolicy {
	if p != nil && p.RetryPolicy != nil {
		return p.RetryPolicy
	}
	return ExponentialRetryPolicy{}
}

func (p *Processor) claimLease() time.Duration {
	if p != nil && p.ClaimLease > 0 {
		return p.ClaimLease
	}
	return 30 * time.Second
}

func (p *Processor) maxAttempts() int {
	if p != nil && p.MaxAttempts > 0 {
		return p.MaxAttempts
	}
	return 8
}

func ensureMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	return metadata
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
