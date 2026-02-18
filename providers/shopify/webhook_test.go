package shopify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/devkit"
	"github.com/goliatone/go-services/webhooks"
)

func TestWebhookTemplate_VerifyExtractAndReplayWindow(t *testing.T) {
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"topic":"products/update"}`)
	cfg := DefaultWebhookConfig("shopify_secret")
	cfg.Now = func() time.Time { return now }
	template := NewWebhookTemplate(cfg)

	req := core.InboundRequest{
		ProviderID: ProviderID,
		Body:       body,
		Headers: map[string]string{
			"X-Shopify-Hmac-Sha256":  signWebhookBody("shopify_secret", body),
			"X-Shopify-Webhook-Id":   "delivery_1",
			"X-Shopify-Triggered-At": now.Format(time.RFC3339),
		},
	}
	if err := template.Verifier.Verify(context.Background(), req); err != nil {
		t.Fatalf("verify webhook: %v", err)
	}
	deliveryID, err := template.Extractor(req)
	if err != nil {
		t.Fatalf("extract delivery id: %v", err)
	}
	if deliveryID != "delivery_1" {
		t.Fatalf("expected delivery id delivery_1, got %q", deliveryID)
	}

	req.Headers["X-Shopify-Triggered-At"] = now.Add(-10 * time.Minute).Format(time.RFC3339)
	if err := template.Verifier.Verify(context.Background(), req); err == nil {
		t.Fatalf("expected stale triggered-at header to fail replay-window check")
	}
}

func TestWebhookTemplate_RejectsMissingDeliveryID(t *testing.T) {
	body := []byte(`{"topic":"products/update"}`)
	template := NewWebhookTemplate(DefaultWebhookConfig("shopify_secret"))

	err := template.Verifier.Verify(context.Background(), core.InboundRequest{
		ProviderID: ProviderID,
		Body:       body,
		Headers: map[string]string{
			"X-Shopify-Hmac-Sha256": signWebhookBody("shopify_secret", body),
		},
	})
	if err == nil {
		t.Fatalf("expected missing delivery id to fail verification")
	}
}

func TestWebhookClaimLifecycle_RetryReadyThenProcessed(t *testing.T) {
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	ledger := devkit.NewWebhookDeliveryLedgerFixture()
	handler := &scriptedWebhookHandler{
		err: []error{context.DeadlineExceeded, nil},
	}

	cfg := DefaultWebhookConfig("shopify_secret")
	cfg.Now = func() time.Time { return now }
	template := NewWebhookTemplate(cfg)
	processor := webhooks.NewProcessor(template.Verifier, ledger, handler)
	processor.ExtractID = template.Extractor
	processor.RetryPolicy = webhooks.ExponentialRetryPolicy{Initial: time.Millisecond, Max: time.Millisecond}
	processor.Now = func() time.Time { return now }

	body := []byte(`{"topic":"products/update"}`)
	req := core.InboundRequest{
		ProviderID: ProviderID,
		Body:       body,
		Headers: map[string]string{
			"X-Shopify-Hmac-Sha256": signWebhookBody("shopify_secret", body),
			"X-Shopify-Webhook-Id":  "delivery_claim_1",
		},
	}

	if _, err := processor.Process(context.Background(), req); err == nil {
		t.Fatalf("expected first attempt to fail and move to retry_ready")
	}
	record, err := ledger.Get(context.Background(), ProviderID, "delivery_claim_1")
	if err != nil {
		t.Fatalf("load delivery record: %v", err)
	}
	if record.Status != webhooks.DeliveryStatusRetryReady {
		t.Fatalf("expected retry_ready after first failure, got %q", record.Status)
	}

	now = now.Add(2 * time.Millisecond)
	result, err := processor.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("process retry-ready delivery: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted result after retry")
	}
	record, err = ledger.Get(context.Background(), ProviderID, "delivery_claim_1")
	if err != nil {
		t.Fatalf("load delivery record: %v", err)
	}
	if record.Status != webhooks.DeliveryStatusProcessed {
		t.Fatalf("expected processed status after retry, got %q", record.Status)
	}
	if record.Attempts != 2 {
		t.Fatalf("expected two attempts, got %d", record.Attempts)
	}
}

type scriptedWebhookHandler struct {
	calls int
	err   []error
}

func (h *scriptedWebhookHandler) Handle(context.Context, core.InboundRequest) (core.InboundResult, error) {
	h.calls++
	if len(h.err) >= h.calls && h.err[h.calls-1] != nil {
		return core.InboundResult{}, h.err[h.calls-1]
	}
	return core.InboundResult{Accepted: true, StatusCode: 202}, nil
}

func signWebhookBody(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
