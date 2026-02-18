package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestProviderWebhookTemplates_VerifyAndExtract(t *testing.T) {
	body := []byte(`{"event":"updated"}`)

	shopify := NewShopifyWebhookTemplate("shopify_secret")
	verifyAndExtractTemplate(t, shopify, core.InboundRequest{
		ProviderID: "shopify",
		Body:       body,
		Headers: map[string]string{
			"X-Shopify-Hmac-Sha256": signBase64HMAC("shopify_secret", body),
			"X-Shopify-Webhook-Id":  "shopify_delivery_1",
		},
	}, "shopify_delivery_1")

	meta := NewMetaWebhookTemplate("meta_secret")
	verifyAndExtractTemplate(t, meta, core.InboundRequest{
		ProviderID: "meta",
		Body:       body,
		Headers: map[string]string{
			"X-Hub-Signature-256": "sha256=" + signHexHMAC("meta_secret", body),
			"X-Meta-Delivery-Id":  "meta_delivery_1",
		},
	}, "meta_delivery_1")

	tiktok := NewTikTokWebhookTemplate("tik_secret")
	verifyAndExtractTemplate(t, tiktok, core.InboundRequest{
		ProviderID: "tiktok",
		Body:       body,
		Headers: map[string]string{
			"X-Tt-Signature":  signHexHMAC("tik_secret", body),
			"X-Tt-Request-Id": "tt_delivery_1",
		},
	}, "tt_delivery_1")

	pinterest := NewPinterestWebhookTemplate("pin_secret")
	verifyAndExtractTemplate(t, pinterest, core.InboundRequest{
		ProviderID: "pinterest",
		Body:       body,
		Headers: map[string]string{
			"X-Pinterest-Hmac-Sha256": signHexHMAC("pin_secret", body),
			"X-Pinterest-Delivery-Id": "pin_delivery_1",
		},
	}, "pin_delivery_1")

	google := NewGoogleWebhookTemplate("google_token")
	verifyAndExtractTemplate(t, google, core.InboundRequest{
		ProviderID: "google",
		Body:       body,
		Headers: map[string]string{
			"X-Goog-Channel-Token":  "google_token",
			"X-Goog-Message-Number": "1001",
		},
	}, "1001")

	amazon := NewAmazonWebhookTemplate("amz_sig")
	verifyAndExtractTemplate(t, amazon, core.InboundRequest{
		ProviderID: "amazon",
		Body:       body,
		Headers: map[string]string{
			"X-Amz-Signature":      "amz_sig",
			"X-Amz-Sns-Message-Id": "amz_delivery_1",
		},
	}, "amz_delivery_1")
}

func TestProviderWebhookTemplates_RejectsInvalidSignature(t *testing.T) {
	template := NewMetaWebhookTemplate("secret")
	err := template.Verifier.Verify(context.Background(), core.InboundRequest{
		ProviderID: "meta",
		Body:       []byte(`{}`),
		Headers: map[string]string{
			"X-Hub-Signature-256": "sha256=bad",
		},
	})
	if err == nil {
		t.Fatalf("expected invalid signature to fail verification")
	}
}

func verifyAndExtractTemplate(
	t *testing.T,
	template ProviderWebhookTemplate,
	req core.InboundRequest,
	expectedDeliveryID string,
) {
	t.Helper()
	if template.Verifier == nil {
		t.Fatalf("expected verifier for template %q", template.ProviderID)
	}
	if template.Extractor == nil {
		t.Fatalf("expected extractor for template %q", template.ProviderID)
	}
	if err := template.Verifier.Verify(context.Background(), req); err != nil {
		t.Fatalf("verify template %q: %v", template.ProviderID, err)
	}
	deliveryID, err := template.Extractor(req)
	if err != nil {
		t.Fatalf("extract delivery id template %q: %v", template.ProviderID, err)
	}
	if deliveryID != expectedDeliveryID {
		t.Fatalf("expected delivery id %q, got %q", expectedDeliveryID, deliveryID)
	}
}

func signHexHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func signBase64HMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
