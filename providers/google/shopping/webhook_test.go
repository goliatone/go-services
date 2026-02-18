package shopping

import (
	"context"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestWebhookTemplate_VerifyExtractAndNormalize(t *testing.T) {
	template := NewWebhookTemplate(DefaultWebhookConfig("goog_token"))
	req := core.InboundRequest{
		ProviderID: ProviderID,
		Headers: map[string]string{
			"X-Goog-Channel-Token":  "goog_token",
			"X-Goog-Message-Number": "9001",
			"X-Goog-Resource-State": "exists",
			"X-Goog-Resource-Id":    "resource_1",
			"X-Goog-Channel-Id":     "channel_1",
			"X-Goog-Changed":        "items",
		},
	}
	if err := template.Verifier.Verify(context.Background(), req); err != nil {
		t.Fatalf("verify webhook: %v", err)
	}
	deliveryID, err := template.Extractor(req)
	if err != nil {
		t.Fatalf("extract delivery id: %v", err)
	}
	if deliveryID != "9001" {
		t.Fatalf("expected delivery id 9001, got %q", deliveryID)
	}

	normalized, err := NormalizeWebhookEvent(req)
	if err != nil {
		t.Fatalf("normalize webhook event: %v", err)
	}
	if normalized.EventType != "exists" {
		t.Fatalf("expected event type exists, got %q", normalized.EventType)
	}
	if normalized.ResourceID != "resource_1" {
		t.Fatalf("expected resource id resource_1, got %q", normalized.ResourceID)
	}
	if !normalized.RequiresPollSync {
		t.Fatalf("expected poll fallback requirement for google shopping webhook")
	}
	guidance := ResolvePollSyncGuidance(normalized)
	if !guidance.Required {
		t.Fatalf("expected poll guidance to be required")
	}
	if guidance.Interval <= 0 {
		t.Fatalf("expected positive poll interval")
	}
}

func TestNormalizeWebhookEvent_RejectsMissingResourceID(t *testing.T) {
	_, err := NormalizeWebhookEvent(core.InboundRequest{
		ProviderID: ProviderID,
		Headers: map[string]string{
			"X-Goog-Message-Number": "9002",
		},
	})
	if err == nil {
		t.Fatalf("expected missing resource id to fail normalization")
	}
}
