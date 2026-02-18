package amazon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/devkit"
	"github.com/goliatone/go-services/webhooks"
)

func TestWebhookTemplate_VerifyExtractAndNormalize(t *testing.T) {
	innerMessage := map[string]any{
		"NotificationType": "ORDER_CHANGE",
		"NotificationMetadata": map[string]any{
			"SellerId": "A1SELLER",
		},
		"Payload": map[string]any{
			"AmazonOrderId": "902-3159896-1390916",
		},
	}
	rawInner, _ := json.Marshal(innerMessage)
	rawEnvelope, _ := json.Marshal(map[string]any{
		"Type":      "Notification",
		"MessageId": "sns_1",
		"Timestamp": "2026-02-18T12:00:00Z",
		"Message":   string(rawInner),
	})

	template := NewWebhookTemplate(DefaultWebhookConfig("signature_token"))
	req := core.InboundRequest{
		ProviderID: ProviderID,
		Body:       rawEnvelope,
		Headers: map[string]string{
			"X-Amz-Signature":      "signature_token",
			"X-Amz-Sns-Message-Id": "sns_1",
		},
	}

	if err := template.Verifier.Verify(context.Background(), req); err != nil {
		t.Fatalf("verify notification: %v", err)
	}
	deliveryID, err := template.Extractor(req)
	if err != nil {
		t.Fatalf("extract delivery id: %v", err)
	}
	if deliveryID != "sns_1" {
		t.Fatalf("expected delivery id sns_1, got %q", deliveryID)
	}

	normalized, err := NormalizeNotification(req)
	if err != nil {
		t.Fatalf("normalize notification: %v", err)
	}
	if normalized.DeliveryID != "sns_1" {
		t.Fatalf("expected normalized delivery id sns_1, got %q", normalized.DeliveryID)
	}
	if normalized.NotificationType != "order_change" {
		t.Fatalf("expected notification type order_change, got %q", normalized.NotificationType)
	}
	if normalized.ResourceID != "902-3159896-1390916" {
		t.Fatalf("expected amazon order id, got %q", normalized.ResourceID)
	}
	if normalized.SellerID != "A1SELLER" {
		t.Fatalf("expected seller id A1SELLER, got %q", normalized.SellerID)
	}
}

func TestNormalizeNotification_RejectsMissingDeliveryID(t *testing.T) {
	_, err := NormalizeNotification(core.InboundRequest{
		ProviderID: ProviderID,
		Body:       []byte(`{"Type":"Notification"}`),
		Headers:    map[string]string{"X-Amz-Signature": "signature_token"},
	})
	if err == nil {
		t.Fatalf("expected missing delivery id error")
	}
}

func TestNormalizeNotification_FallbackPollSyncWhenResourceMissing(t *testing.T) {
	rawEnvelope, _ := json.Marshal(map[string]any{
		"Type":      "Notification",
		"MessageId": "sns_2",
		"Message":   `{"NotificationType":"INVENTORY_CHANGE"}`,
	})
	normalized, err := NormalizeNotification(core.InboundRequest{
		ProviderID: ProviderID,
		Body:       rawEnvelope,
		Headers: map[string]string{
			"X-Amz-Sns-Message-Id": "sns_2",
		},
	})
	if err != nil {
		t.Fatalf("normalize notification: %v", err)
	}
	if !normalized.RequiresPollSync {
		t.Fatalf("expected poll sync requirement when resource id is missing")
	}
	if normalized.ResourceID != "" {
		t.Fatalf("expected missing resource id, got %q", normalized.ResourceID)
	}
	guidance := ResolvePollSyncGuidance(normalized)
	if !guidance.Required {
		t.Fatalf("expected poll sync guidance to be required")
	}
	if guidance.Interval <= 0 {
		t.Fatalf("expected positive poll sync interval")
	}
}

func TestWebhookClaimLifecycle_RetryReadyThenProcessed(t *testing.T) {
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	ledger := devkit.NewWebhookDeliveryLedgerFixture()
	handler := &amazonScriptedWebhookHandler{
		err: []error{context.DeadlineExceeded, nil},
	}

	template := NewWebhookTemplate(DefaultWebhookConfig("signature_token"))
	processor := webhooks.NewProcessor(template.Verifier, ledger, handler)
	processor.ExtractID = template.Extractor
	processor.RetryPolicy = webhooks.ExponentialRetryPolicy{Initial: time.Millisecond, Max: time.Millisecond}
	processor.Now = func() time.Time { return now }

	body := []byte(`{"Type":"Notification","MessageId":"sns_claim_1","Message":"{\"NotificationType\":\"ORDER_CHANGE\"}"}`)
	req := core.InboundRequest{
		ProviderID: ProviderID,
		Body:       body,
		Headers: map[string]string{
			"X-Amz-Signature":      "signature_token",
			"X-Amz-Sns-Message-Id": "sns_claim_1",
		},
	}

	if _, err := processor.Process(context.Background(), req); err == nil {
		t.Fatalf("expected first attempt to fail and move to retry_ready")
	}
	record, err := ledger.Get(context.Background(), ProviderID, "sns_claim_1")
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
	record, err = ledger.Get(context.Background(), ProviderID, "sns_claim_1")
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

type amazonScriptedWebhookHandler struct {
	calls int
	err   []error
}

func (h *amazonScriptedWebhookHandler) Handle(context.Context, core.InboundRequest) (core.InboundResult, error) {
	h.calls++
	if len(h.err) >= h.calls && h.err[h.calls-1] != nil {
		return core.InboundResult{}, h.err[h.calls-1]
	}
	return core.InboundResult{Accepted: true, StatusCode: 202}, nil
}
