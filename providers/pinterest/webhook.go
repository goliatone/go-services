package pinterest

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/webhooks"
)

type WebhookConfig struct {
	Secret string
}

type PollSyncGuidance struct {
	Required bool
	Interval time.Duration
	Reason   string
}

type NormalizedWebhookEvent struct {
	ProviderID       string
	DeliveryID       string
	EventType        string
	ResourceID       string
	RequiresPollSync bool
	PollSyncReason   string
}

type rawWebhookPayload struct {
	EventType string         `json:"event_type"`
	Event     string         `json:"event"`
	EntityID  string         `json:"entity_id"`
	Data      map[string]any `json:"data"`
}

func DefaultWebhookConfig(secret string) WebhookConfig {
	return WebhookConfig{Secret: strings.TrimSpace(secret)}
}

func NewWebhookTemplate(cfg WebhookConfig) webhooks.ProviderWebhookTemplate {
	template := webhooks.NewPinterestWebhookTemplate(cfg.Secret)
	template.ProviderID = ProviderID
	return template
}

func NormalizeWebhookEvent(req core.InboundRequest) (NormalizedWebhookEvent, error) {
	deliveryID, err := webhooks.HeaderDeliveryIDExtractor("X-Pinterest-Delivery-Id", "X-Pinterest-Request-Id")(req)
	if err != nil {
		return NormalizedWebhookEvent{}, err
	}

	payload := rawWebhookPayload{}
	if len(req.Body) > 0 {
		if err := json.Unmarshal(req.Body, &payload); err != nil {
			return NormalizedWebhookEvent{}, fmt.Errorf("providers/pinterest: parse webhook payload: %w", err)
		}
	}

	eventType := strings.TrimSpace(strings.ToLower(firstNonEmpty(payload.EventType, payload.Event)))
	if eventType == "" {
		eventType = "unknown"
	}
	resourceID := strings.TrimSpace(payload.EntityID)
	if resourceID == "" {
		resourceID = strings.TrimSpace(readAnyString(payload.Data["entity_id"]))
	}
	if resourceID == "" {
		resourceID = strings.TrimSpace(readAnyString(payload.Data["pin_id"]))
	}
	if resourceID == "" {
		resourceID = strings.TrimSpace(readAnyString(payload.Data["board_id"]))
	}

	requiresPoll := resourceID == "" || strings.HasPrefix(eventType, "board.")
	reason := ""
	if requiresPoll {
		reason = "pinterest webhook payload is insufficient for deterministic incremental processing; run poll sync"
	}

	return NormalizedWebhookEvent{
		ProviderID:       ProviderID,
		DeliveryID:       deliveryID,
		EventType:        eventType,
		ResourceID:       resourceID,
		RequiresPollSync: requiresPoll,
		PollSyncReason:   reason,
	}, nil
}

func ResolvePollSyncGuidance(event NormalizedWebhookEvent) PollSyncGuidance {
	if event.RequiresPollSync {
		return PollSyncGuidance{
			Required: true,
			Interval: 10 * time.Minute,
			Reason:   event.PollSyncReason,
		}
	}
	if strings.HasPrefix(event.EventType, "pin.") {
		return PollSyncGuidance{
			Required: false,
			Interval: 20 * time.Minute,
			Reason:   "pinterest pin events should be periodically reconciled with poll sync",
		}
	}
	return PollSyncGuidance{
		Required: false,
		Interval: 30 * time.Minute,
		Reason:   "pinterest event should be reconciled with scheduled poll sync",
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func readAnyString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
