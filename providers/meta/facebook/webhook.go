package facebook

import (
	"github.com/goliatone/go-services/core"
	meta "github.com/goliatone/go-services/providers/meta/common"
	"github.com/goliatone/go-services/webhooks"
)

type WebhookConfig = meta.WebhookConfig

type NormalizedWebhookEvent = meta.NormalizedWebhookEvent

func DefaultWebhookConfig(secret string) WebhookConfig {
	return meta.DefaultWebhookConfig(secret)
}

func NewWebhookTemplate(cfg WebhookConfig) webhooks.ProviderWebhookTemplate {
	return meta.NewWebhookTemplate(ProviderID, cfg)
}

func NormalizeWebhookEvent(req core.InboundRequest) (NormalizedWebhookEvent, error) {
	return meta.NormalizeWebhookEvent(ProviderID, req)
}
