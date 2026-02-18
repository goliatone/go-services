package shopify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/webhooks"
)

const (
	shopifyHeaderHMAC       = "X-Shopify-Hmac-Sha256"
	shopifyHeaderDeliveryID = "X-Shopify-Webhook-Id"
	shopifyHeaderTriggered  = "X-Shopify-Triggered-At"
)

const defaultWebhookReplayWindow = 5 * time.Minute

type WebhookConfig struct {
	Secret             string
	ReplayWindow       time.Duration
	Now                func() time.Time
	RequireTriggeredAt bool
}

func DefaultWebhookConfig(secret string) WebhookConfig {
	return WebhookConfig{
		Secret:       strings.TrimSpace(secret),
		ReplayWindow: defaultWebhookReplayWindow,
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func NewWebhookTemplate(cfg WebhookConfig) webhooks.ProviderWebhookTemplate {
	return webhooks.ProviderWebhookTemplate{
		ProviderID: ProviderID,
		Verifier: ShopifyWebhookVerifier{
			Secret:             strings.TrimSpace(cfg.Secret),
			ReplayWindow:       cfg.ReplayWindow,
			Now:                cfg.Now,
			RequireTriggeredAt: cfg.RequireTriggeredAt,
		},
		Extractor: ExtractDeliveryID,
	}
}

type ShopifyWebhookVerifier struct {
	Secret             string
	ReplayWindow       time.Duration
	Now                func() time.Time
	RequireTriggeredAt bool
}

func (v ShopifyWebhookVerifier) Verify(ctx context.Context, req core.InboundRequest) error {
	sigVerifier := webhooks.HeaderHMACVerifier{
		Header:   shopifyHeaderHMAC,
		Secret:   strings.TrimSpace(v.Secret),
		Encoding: "base64",
	}
	if err := sigVerifier.Verify(ctx, req); err != nil {
		return err
	}

	deliveryID, err := ExtractDeliveryID(req)
	if err != nil {
		return err
	}
	if strings.TrimSpace(deliveryID) == "" {
		return fmt.Errorf("providers/shopify: delivery id is required")
	}

	triggered := strings.TrimSpace(headerValue(req.Headers, shopifyHeaderTriggered))
	if triggered == "" {
		if v.RequireTriggeredAt {
			return fmt.Errorf("providers/shopify: %s header is required", shopifyHeaderTriggered)
		}
		return nil
	}
	triggeredAt, err := time.Parse(time.RFC3339Nano, triggered)
	if err != nil {
		return fmt.Errorf("providers/shopify: parse %s: %w", shopifyHeaderTriggered, err)
	}

	now := time.Now().UTC()
	if v.Now != nil {
		now = v.Now().UTC()
	}
	window := v.ReplayWindow
	if window <= 0 {
		window = defaultWebhookReplayWindow
	}
	delta := now.Sub(triggeredAt.UTC())
	if delta < 0 {
		delta = -delta
	}
	if delta > window {
		return fmt.Errorf("providers/shopify: webhook trigger time outside replay window")
	}
	return nil
}

func ExtractDeliveryID(req core.InboundRequest) (string, error) {
	deliveryID := strings.TrimSpace(headerValue(req.Headers, shopifyHeaderDeliveryID))
	if deliveryID == "" {
		return "", fmt.Errorf("providers/shopify: %s header is required for dedupe", shopifyHeaderDeliveryID)
	}
	return deliveryID, nil
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
