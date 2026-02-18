package devkit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/webhooks"
)

type SigV4Fixture struct {
	Request      *http.Request
	Credential   core.ActiveCredential
	ExpectMode   string
	ExpectRegion string
	ExpectSvc    string
}

func NewSigV4HeaderFixture() SigV4Fixture {
	req, _ := http.NewRequest(http.MethodGet, "https://sellingpartnerapi-na.amazon.com/orders/v0/orders?MarketplaceIds=ATVPDKIKX0DER", nil)
	cred := core.ActiveCredential{
		AccessToken: "lwa_access_token",
		Metadata: map[string]any{
			"auth_kind":               core.AuthKindAWSSigV4,
			"aws_access_key_id":       "AKIAEXAMPLE",
			"aws_secret_access_key":   "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
			"aws_session_token":       "session-token-1",
			"aws_region":              "us-east-1",
			"aws_service":             "execute-api",
			"aws_signing_mode":        "header",
			"aws_access_token_header": "x-amz-access-token",
		},
	}
	return SigV4Fixture{
		Request:      req,
		Credential:   cred,
		ExpectMode:   "header",
		ExpectRegion: "us-east-1",
		ExpectSvc:    "execute-api",
	}
}

func NewSigV4QueryFixture() SigV4Fixture {
	req, _ := http.NewRequest(http.MethodGet, "https://sellingpartnerapi-na.amazon.com/orders/v0/orders", nil)
	cred := core.ActiveCredential{
		Metadata: map[string]any{
			"auth_kind":             core.AuthKindAWSSigV4,
			"aws_access_key_id":     "AKIAEXAMPLE",
			"aws_secret_access_key": "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
			"aws_region":            "us-west-2",
			"aws_service":           "execute-api",
			"aws_signing_mode":      "query",
			"aws_signing_expires":   "120",
		},
	}
	return SigV4Fixture{
		Request:      req,
		Credential:   cred,
		ExpectMode:   "query",
		ExpectRegion: "us-west-2",
		ExpectSvc:    "execute-api",
	}
}

type WebhookTemplateFixture struct {
	Name       string
	Template   webhooks.ProviderWebhookTemplate
	Request    core.InboundRequest
	DeliveryID string
}

func NewWebhookTemplateFixtures() []WebhookTemplateFixture {
	body := []byte(`{"event":"sync"}`)
	return []WebhookTemplateFixture{
		{
			Name:     "shopify",
			Template: webhooks.NewShopifyWebhookTemplate("shopify_secret"),
			Request: core.InboundRequest{
				ProviderID: "shopify",
				Body:       body,
				Headers: map[string]string{
					"X-Shopify-Hmac-Sha256": signBase64HMACFixture("shopify_secret", body),
					"X-Shopify-Webhook-Id":  "shopify_1",
				},
			},
			DeliveryID: "shopify_1",
		},
		{
			Name:     "meta",
			Template: webhooks.NewMetaWebhookTemplate("meta_secret"),
			Request: core.InboundRequest{
				ProviderID: "meta",
				Body:       body,
				Headers: map[string]string{
					"X-Hub-Signature-256": "sha256=" + signHexHMACFixture("meta_secret", body),
					"X-Meta-Delivery-Id":  "meta_1",
				},
			},
			DeliveryID: "meta_1",
		},
		{
			Name:     "tiktok",
			Template: webhooks.NewTikTokWebhookTemplate("tik_secret"),
			Request: core.InboundRequest{
				ProviderID: "tiktok",
				Body:       body,
				Headers: map[string]string{
					"X-Tt-Signature":  signHexHMACFixture("tik_secret", body),
					"X-Tt-Request-Id": "tt_1",
				},
			},
			DeliveryID: "tt_1",
		},
		{
			Name:     "pinterest",
			Template: webhooks.NewPinterestWebhookTemplate("pin_secret"),
			Request: core.InboundRequest{
				ProviderID: "pinterest",
				Body:       body,
				Headers: map[string]string{
					"X-Pinterest-Hmac-Sha256": signHexHMACFixture("pin_secret", body),
					"X-Pinterest-Delivery-Id": "pin_1",
				},
			},
			DeliveryID: "pin_1",
		},
		{
			Name:     "google",
			Template: webhooks.NewGoogleWebhookTemplate("goog_token"),
			Request: core.InboundRequest{
				ProviderID: "google",
				Body:       body,
				Headers: map[string]string{
					"X-Goog-Channel-Token":  "goog_token",
					"X-Goog-Message-Number": "9001",
				},
			},
			DeliveryID: "9001",
		},
		{
			Name:     "amazon",
			Template: webhooks.NewAmazonWebhookTemplate("amz_signature"),
			Request: core.InboundRequest{
				ProviderID: "amazon",
				Body:       body,
				Headers: map[string]string{
					"X-Amz-Signature":      "amz_signature",
					"X-Amz-Sns-Message-Id": "amz_1",
				},
			},
			DeliveryID: "amz_1",
		},
	}
}

func signHexHMACFixture(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func signBase64HMACFixture(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
