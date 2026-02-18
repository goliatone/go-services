package amazon

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/webhooks"
)

type WebhookConfig struct {
	SignatureToken string
}

type PollSyncGuidance struct {
	Required bool
	Interval time.Duration
	Reason   string
}

type NormalizedNotification struct {
	ProviderID       string
	DeliveryID       string
	NotificationType string
	ResourceID       string
	SellerID         string
	EventAt          string
	RequiresPollSync bool
	PollSyncReason   string
}

type rawNotificationEnvelope struct {
	Type      string `json:"Type"`
	MessageID string `json:"MessageId"`
	Subject   string `json:"Subject"`
	Timestamp string `json:"Timestamp"`
	Message   string `json:"Message"`
}

func DefaultWebhookConfig(signatureToken string) WebhookConfig {
	return WebhookConfig{SignatureToken: strings.TrimSpace(signatureToken)}
}

func NewWebhookTemplate(cfg WebhookConfig) webhooks.ProviderWebhookTemplate {
	template := webhooks.NewAmazonWebhookTemplate(cfg.SignatureToken)
	template.ProviderID = ProviderID
	return template
}

func NormalizeNotification(req core.InboundRequest) (NormalizedNotification, error) {
	deliveryID, err := webhooks.HeaderDeliveryIDExtractor("X-Amz-Sns-Message-Id", "X-Amz-Request-Id")(req)
	if err != nil {
		return NormalizedNotification{}, err
	}

	envelope := rawNotificationEnvelope{}
	if len(req.Body) > 0 {
		if err := json.Unmarshal(req.Body, &envelope); err != nil {
			return NormalizedNotification{}, fmt.Errorf("providers/amazon: parse notification envelope: %w", err)
		}
	}
	message := map[string]any{}
	if rawMessage := strings.TrimSpace(envelope.Message); rawMessage != "" {
		_ = json.Unmarshal([]byte(rawMessage), &message)
	}

	notificationType := strings.TrimSpace(strings.ToLower(firstNonEmpty(
		readString(message, "NotificationType", "notificationType", "event_type"),
		envelope.Subject,
		headerValue(req.Headers, "X-Amz-Notification-Type"),
	)))
	if notificationType == "" {
		notificationType = "unknown"
	}

	resourceID := strings.TrimSpace(firstNonEmpty(
		readNestedString(message, "Payload", "AmazonOrderId"),
		readNestedString(message, "Payload", "Asin"),
		readNestedString(message, "NotificationMetadata", "OrderId"),
		readString(message, "amazon_order_id", "order_id", "resource_id"),
	))
	sellerID := strings.TrimSpace(firstNonEmpty(
		readNestedString(message, "NotificationMetadata", "SellerId"),
		readNestedString(message, "Payload", "SellerId"),
		readString(message, "seller_id", "SellerId"),
	))

	requiresPoll := true
	reason := "amazon notifications provide event signals; run poll sync for deterministic SP-API state"
	if resourceID == "" {
		reason = "amazon notification missing stable resource id; run poll sync fallback"
	}

	return NormalizedNotification{
		ProviderID:       ProviderID,
		DeliveryID:       deliveryID,
		NotificationType: notificationType,
		ResourceID:       resourceID,
		SellerID:         sellerID,
		EventAt:          strings.TrimSpace(envelope.Timestamp),
		RequiresPollSync: requiresPoll,
		PollSyncReason:   reason,
	}, nil
}

func ResolvePollSyncGuidance(event NormalizedNotification) PollSyncGuidance {
	interval := 15 * time.Minute
	switch {
	case strings.Contains(event.NotificationType, "order"):
		interval = 2 * time.Minute
	case strings.Contains(event.NotificationType, "inventory"):
		interval = 5 * time.Minute
	case strings.Contains(event.NotificationType, "catalog"):
		interval = 10 * time.Minute
	}
	if event.ResourceID == "" {
		interval = 2 * time.Minute
	}
	return PollSyncGuidance{
		Required: event.RequiresPollSync,
		Interval: interval,
		Reason:   event.PollSyncReason,
	}
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

func readNestedString(metadata map[string]any, parent string, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata[parent]
	if !ok || raw == nil {
		return ""
	}
	nested, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	return readString(nested, key)
}
