package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/goliatone/go-services/core"
)

type ProviderWebhookTemplate struct {
	ProviderID string
	Verifier   Verifier
	Extractor  DeliveryIDExtractor
}

type HeaderHMACVerifier struct {
	Header   string
	Prefix   string
	Secret   string
	Encoding string // hex | base64
}

func (v HeaderHMACVerifier) Verify(_ context.Context, req core.InboundRequest) error {
	header := strings.TrimSpace(headerValue(req.Headers, v.Header))
	if header == "" {
		return fmt.Errorf("webhooks: %s signature header is required", strings.TrimSpace(v.Header))
	}
	secret := strings.TrimSpace(v.Secret)
	if secret == "" {
		return fmt.Errorf("webhooks: signature secret is required")
	}
	signature := strings.TrimPrefix(header, strings.TrimSpace(v.Prefix))
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return fmt.Errorf("webhooks: signature value is required")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(req.Body)
	expected := mac.Sum(nil)

	switch strings.ToLower(strings.TrimSpace(v.Encoding)) {
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(signature)
		if err != nil {
			return fmt.Errorf("webhooks: decode base64 signature: %w", err)
		}
		if subtle.ConstantTimeCompare(decoded, expected) != 1 {
			return fmt.Errorf("webhooks: signature verification failed")
		}
	default:
		decoded, err := hex.DecodeString(signature)
		if err != nil {
			return fmt.Errorf("webhooks: decode hex signature: %w", err)
		}
		if subtle.ConstantTimeCompare(decoded, expected) != 1 {
			return fmt.Errorf("webhooks: signature verification failed")
		}
	}
	return nil
}

type HeaderTokenVerifier struct {
	Header string
	Token  string
}

func (v HeaderTokenVerifier) Verify(_ context.Context, req core.InboundRequest) error {
	expected := strings.TrimSpace(v.Token)
	if expected == "" {
		return fmt.Errorf("webhooks: verification token is required")
	}
	actual := strings.TrimSpace(headerValue(req.Headers, v.Header))
	if actual == "" {
		return fmt.Errorf("webhooks: %s verification header is required", strings.TrimSpace(v.Header))
	}
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return fmt.Errorf("webhooks: verification token mismatch")
	}
	return nil
}

func HeaderDeliveryIDExtractor(headers ...string) DeliveryIDExtractor {
	keys := append([]string(nil), headers...)
	return func(req core.InboundRequest) (string, error) {
		for _, key := range keys {
			if value := strings.TrimSpace(headerValue(req.Headers, key)); value != "" {
				return value, nil
			}
		}
		return "", fmt.Errorf("webhooks: delivery id is required for dedupe")
	}
}

func ChainDeliveryIDExtractors(extractors ...DeliveryIDExtractor) DeliveryIDExtractor {
	list := append([]DeliveryIDExtractor(nil), extractors...)
	return func(req core.InboundRequest) (string, error) {
		var lastErr error
		for _, extractor := range list {
			if extractor == nil {
				continue
			}
			deliveryID, err := extractor(req)
			if err == nil && strings.TrimSpace(deliveryID) != "" {
				return strings.TrimSpace(deliveryID), nil
			}
			if err != nil {
				lastErr = err
			}
		}
		if lastErr != nil {
			return "", lastErr
		}
		return "", fmt.Errorf("webhooks: delivery id is required for dedupe")
	}
}

func NewShopifyWebhookTemplate(secret string) ProviderWebhookTemplate {
	return ProviderWebhookTemplate{
		ProviderID: "shopify",
		Verifier: HeaderHMACVerifier{
			Header:   "X-Shopify-Hmac-Sha256",
			Secret:   strings.TrimSpace(secret),
			Encoding: "base64",
		},
		Extractor: HeaderDeliveryIDExtractor("X-Shopify-Webhook-Id", "X-Request-Id"),
	}
}

func NewMetaWebhookTemplate(secret string) ProviderWebhookTemplate {
	return ProviderWebhookTemplate{
		ProviderID: "meta",
		Verifier: HeaderHMACVerifier{
			Header:   "X-Hub-Signature-256",
			Prefix:   "sha256=",
			Secret:   strings.TrimSpace(secret),
			Encoding: "hex",
		},
		Extractor: HeaderDeliveryIDExtractor("X-Meta-Delivery-Id", "X-Hub-Signature-256"),
	}
}

func NewTikTokWebhookTemplate(secret string) ProviderWebhookTemplate {
	return ProviderWebhookTemplate{
		ProviderID: "tiktok",
		Verifier: HeaderHMACVerifier{
			Header:   "X-Tt-Signature",
			Secret:   strings.TrimSpace(secret),
			Encoding: "hex",
		},
		Extractor: HeaderDeliveryIDExtractor("X-Tt-Request-Id", "X-Tt-Logid"),
	}
}

func NewPinterestWebhookTemplate(secret string) ProviderWebhookTemplate {
	return ProviderWebhookTemplate{
		ProviderID: "pinterest",
		Verifier: HeaderHMACVerifier{
			Header:   "X-Pinterest-Hmac-Sha256",
			Secret:   strings.TrimSpace(secret),
			Encoding: "hex",
		},
		Extractor: HeaderDeliveryIDExtractor("X-Pinterest-Delivery-Id", "X-Pinterest-Request-Id"),
	}
}

func NewGoogleWebhookTemplate(channelToken string) ProviderWebhookTemplate {
	return ProviderWebhookTemplate{
		ProviderID: "google",
		Verifier: HeaderTokenVerifier{
			Header: "X-Goog-Channel-Token",
			Token:  strings.TrimSpace(channelToken),
		},
		Extractor: HeaderDeliveryIDExtractor("X-Goog-Message-Number", "X-Goog-Resource-Id"),
	}
}

func NewAmazonWebhookTemplate(signatureToken string) ProviderWebhookTemplate {
	return ProviderWebhookTemplate{
		ProviderID: "amazon",
		Verifier: HeaderTokenVerifier{
			Header: "X-Amz-Signature",
			Token:  strings.TrimSpace(signatureToken),
		},
		Extractor: HeaderDeliveryIDExtractor("X-Amz-Sns-Message-Id", "X-Amz-Request-Id"),
	}
}
