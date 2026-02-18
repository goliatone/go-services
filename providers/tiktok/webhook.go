package tiktok

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
	Event string         `json:"event"`
	Type  string         `json:"type"`
	Data  map[string]any `json:"data"`
}

func DefaultWebhookConfig(secret string) WebhookConfig {
	return WebhookConfig{Secret: strings.TrimSpace(secret)}
}

func NewWebhookTemplate(cfg WebhookConfig) webhooks.ProviderWebhookTemplate {
	template := webhooks.NewTikTokWebhookTemplate(cfg.Secret)
	template.ProviderID = ProviderID
	return template
}

func NormalizeWebhookEvent(req core.InboundRequest) (NormalizedWebhookEvent, error) {
	deliveryID, err := webhooks.HeaderDeliveryIDExtractor("X-Tt-Request-Id", "X-Tt-Logid")(req)
	if err != nil {
		return NormalizedWebhookEvent{}, err
	}

	payload := rawWebhookPayload{}
	if len(req.Body) > 0 {
		if err := json.Unmarshal(req.Body, &payload); err != nil {
			return NormalizedWebhookEvent{}, fmt.Errorf("providers/tiktok: parse webhook payload: %w", err)
		}
	}

	eventType := strings.TrimSpace(strings.ToLower(firstNonEmpty(payload.Event, payload.Type)))
	if eventType == "" {
		eventType = "unknown"
	}
	resourceID := strings.TrimSpace(readAnyString(payload.Data["video_id"]))
	if resourceID == "" {
		resourceID = strings.TrimSpace(readAnyString(payload.Data["id"]))
	}
	if resourceID == "" {
		resourceID = strings.TrimSpace(readAnyString(payload.Data["user_id"]))
	}

	requiresPoll := resourceID == ""
	reason := ""
	if requiresPoll {
		reason = "tiktok webhook payload missing stable resource id; use poll sync fallback"
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
			Interval: 5 * time.Minute,
			Reason:   event.PollSyncReason,
		}
	}
	if strings.Contains(event.EventType, "video") {
		return PollSyncGuidance{
			Required: false,
			Interval: 15 * time.Minute,
			Reason:   "tiktok webhook should be confirmed with periodic poll sync",
		}
	}
	return PollSyncGuidance{
		Required: false,
		Interval: 30 * time.Minute,
		Reason:   "tiktok event type has limited payload context",
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
