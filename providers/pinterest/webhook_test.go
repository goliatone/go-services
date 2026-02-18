package pinterest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestWebhookTemplate_VerifyExtractAndNormalize(t *testing.T) {
	body := []byte(`{"event_type":"pin.create","entity_id":"pin_1"}`)
	template := NewWebhookTemplate(DefaultWebhookConfig("pin_secret"))

	req := core.InboundRequest{
		ProviderID: ProviderID,
		Body:       body,
		Headers: map[string]string{
			"X-Pinterest-Hmac-Sha256": signWebhookBody("pin_secret", body),
			"X-Pinterest-Delivery-Id": "pin_delivery_1",
		},
	}
	if err := template.Verifier.Verify(context.Background(), req); err != nil {
		t.Fatalf("verify webhook: %v", err)
	}
	deliveryID, err := template.Extractor(req)
	if err != nil {
		t.Fatalf("extract delivery id: %v", err)
	}
	if deliveryID != "pin_delivery_1" {
		t.Fatalf("expected delivery id pin_delivery_1, got %q", deliveryID)
	}

	normalized, err := NormalizeWebhookEvent(req)
	if err != nil {
		t.Fatalf("normalize webhook event: %v", err)
	}
	if normalized.EventType != "pin.create" {
		t.Fatalf("expected event type pin.create, got %q", normalized.EventType)
	}
	if normalized.ResourceID != "pin_1" {
		t.Fatalf("expected resource id pin_1, got %q", normalized.ResourceID)
	}
	guidance := ResolvePollSyncGuidance(normalized)
	if guidance.Required {
		t.Fatalf("expected poll fallback not required for pin event with entity id")
	}
}

func TestNormalizeWebhookEvent_FallbackPollSyncWhenBoardEventOrMissingResource(t *testing.T) {
	body := []byte(`{"event_type":"board.update","data":{}}`)
	req := core.InboundRequest{
		ProviderID: ProviderID,
		Body:       body,
		Headers: map[string]string{
			"X-Pinterest-Delivery-Id": "pin_delivery_2",
		},
	}

	normalized, err := NormalizeWebhookEvent(req)
	if err != nil {
		t.Fatalf("normalize webhook event: %v", err)
	}
	if !normalized.RequiresPollSync {
		t.Fatalf("expected poll fallback requirement for board events")
	}
	guidance := ResolvePollSyncGuidance(normalized)
	if !guidance.Required {
		t.Fatalf("expected poll guidance to be required")
	}
	if guidance.Interval <= 0 {
		t.Fatalf("expected positive poll interval")
	}
}

func signWebhookBody(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
