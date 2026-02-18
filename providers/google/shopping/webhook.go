package shopping

import (
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/webhooks"
)

type WebhookConfig struct {
	ChannelToken string
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
	ChannelID        string
	ChangedHint      string
	RequiresPollSync bool
	PollSyncReason   string
}

func DefaultWebhookConfig(channelToken string) WebhookConfig {
	return WebhookConfig{ChannelToken: strings.TrimSpace(channelToken)}
}

func NewWebhookTemplate(cfg WebhookConfig) webhooks.ProviderWebhookTemplate {
	template := webhooks.NewGoogleWebhookTemplate(cfg.ChannelToken)
	template.ProviderID = ProviderID
	return template
}

func NormalizeWebhookEvent(req core.InboundRequest) (NormalizedWebhookEvent, error) {
	deliveryID, err := webhooks.HeaderDeliveryIDExtractor("X-Goog-Message-Number", "X-Goog-Resource-Id")(req)
	if err != nil {
		return NormalizedWebhookEvent{}, err
	}

	eventType := strings.TrimSpace(strings.ToLower(headerValue(req.Headers, "X-Goog-Resource-State")))
	if eventType == "" {
		eventType = "sync"
	}
	resourceID := strings.TrimSpace(headerValue(req.Headers, "X-Goog-Resource-Id"))
	if resourceID == "" {
		return NormalizedWebhookEvent{}, fmt.Errorf("providers/google/shopping: X-Goog-Resource-Id header is required")
	}

	reason := "google channel notifications only provide change hints; run poll sync for deterministic state"
	return NormalizedWebhookEvent{
		ProviderID:       ProviderID,
		DeliveryID:       deliveryID,
		EventType:        eventType,
		ResourceID:       resourceID,
		ChannelID:        strings.TrimSpace(headerValue(req.Headers, "X-Goog-Channel-Id")),
		ChangedHint:      strings.TrimSpace(strings.ToLower(headerValue(req.Headers, "X-Goog-Changed"))),
		RequiresPollSync: true,
		PollSyncReason:   reason,
	}, nil
}

func ResolvePollSyncGuidance(event NormalizedWebhookEvent) PollSyncGuidance {
	interval := 5 * time.Minute
	switch event.EventType {
	case "sync":
		interval = 15 * time.Minute
	case "exists", "update", "not_exists":
		interval = 2 * time.Minute
	}
	return PollSyncGuidance{
		Required: true,
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
