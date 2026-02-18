package common

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestNewWebhookTemplate_VerifyAndExtractForInstagramAndFacebook(t *testing.T) {
	cases := []struct {
		name       string
		providerID string
		body       []byte
		deliveryID string
	}{
		{
			name:       "instagram",
			providerID: "meta_instagram",
			body:       []byte(`{"object":"instagram","entry":[{"changes":[{"field":"comments"}]}]}`),
			deliveryID: "meta_ig_1",
		},
		{
			name:       "facebook",
			providerID: "meta_facebook",
			body:       []byte(`{"object":"page","entry":[{"changes":[{"field":"feed"}]}]}`),
			deliveryID: "meta_fb_1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			template := NewWebhookTemplate(tc.providerID, DefaultWebhookConfig("meta_secret"))
			if template.ProviderID != tc.providerID {
				t.Fatalf("expected template provider id %q, got %q", tc.providerID, template.ProviderID)
			}
			req := core.InboundRequest{
				ProviderID: tc.providerID,
				Body:       tc.body,
				Headers: map[string]string{
					"X-Hub-Signature-256": "sha256=" + signWebhookBody("meta_secret", tc.body),
					"X-Meta-Delivery-Id":  tc.deliveryID,
				},
			}
			if err := template.Verifier.Verify(context.Background(), req); err != nil {
				t.Fatalf("verify webhook: %v", err)
			}
			deliveryID, err := template.Extractor(req)
			if err != nil {
				t.Fatalf("extract delivery id: %v", err)
			}
			if deliveryID != tc.deliveryID {
				t.Fatalf("expected delivery id %q, got %q", tc.deliveryID, deliveryID)
			}
		})
	}
}

func TestNormalizeWebhookEvent_EnforcesProviderSpecificObjectRules(t *testing.T) {
	instagramReq := core.InboundRequest{
		ProviderID: "meta_instagram",
		Body:       []byte(`{"object":"instagram","entry":[{"changes":[{"field":"comments"},{"field":"mentions"}]}]}`),
		Headers: map[string]string{
			"X-Meta-Delivery-Id": "delivery_instagram_1",
		},
	}
	normalized, err := NormalizeWebhookEvent("meta_instagram", instagramReq)
	if err != nil {
		t.Fatalf("normalize instagram webhook: %v", err)
	}
	if normalized.Object != "instagram" {
		t.Fatalf("expected object instagram, got %q", normalized.Object)
	}
	if normalized.EntryCount != 1 {
		t.Fatalf("expected one entry, got %d", normalized.EntryCount)
	}
	expectedFields := []string{"comments", "mentions"}
	if len(normalized.ChangeFields) != len(expectedFields) {
		t.Fatalf("expected %d change fields, got %d (%v)", len(expectedFields), len(normalized.ChangeFields), normalized.ChangeFields)
	}
	for idx := range expectedFields {
		if normalized.ChangeFields[idx] != expectedFields[idx] {
			t.Fatalf("expected field %q at index %d, got %q", expectedFields[idx], idx, normalized.ChangeFields[idx])
		}
	}

	facebookReq := core.InboundRequest{
		ProviderID: "meta_facebook",
		Body:       []byte(`{"object":"page","entry":[{"changes":[{"field":"feed"}]}]}`),
		Headers: map[string]string{
			"X-Meta-Delivery-Id": "delivery_facebook_1",
		},
	}
	if _, err := NormalizeWebhookEvent("meta_facebook", facebookReq); err != nil {
		t.Fatalf("normalize facebook webhook: %v", err)
	}

	_, err = NormalizeWebhookEvent("meta_instagram", facebookReq)
	if err == nil {
		t.Fatalf("expected provider/object mismatch to fail normalization")
	}
}

func signWebhookBody(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
