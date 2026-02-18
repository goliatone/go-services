package common

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/webhooks"
)

const (
	providerIDMeta      = "meta"
	providerIDInstagram = "meta_instagram"
	providerIDFacebook  = "meta_facebook"
)

type WebhookConfig struct {
	Secret string
}

type NormalizedWebhookEvent struct {
	ProviderID   string
	DeliveryID   string
	Object       string
	EntryCount   int
	ChangeFields []string
}

type rawWebhookEnvelope struct {
	Object string            `json:"object"`
	Entry  []rawWebhookEntry `json:"entry"`
}

type rawWebhookEntry struct {
	Changes []rawWebhookChange `json:"changes"`
}

type rawWebhookChange struct {
	Field string `json:"field"`
}

func DefaultWebhookConfig(secret string) WebhookConfig {
	return WebhookConfig{Secret: strings.TrimSpace(secret)}
}

func NewWebhookTemplate(providerID string, cfg WebhookConfig) webhooks.ProviderWebhookTemplate {
	template := webhooks.NewMetaWebhookTemplate(cfg.Secret)
	template.ProviderID = strings.TrimSpace(strings.ToLower(providerID))
	return template
}

func NormalizeWebhookEvent(providerID string, req core.InboundRequest) (NormalizedWebhookEvent, error) {
	providerID = strings.TrimSpace(strings.ToLower(providerID))
	if providerID == "" {
		return NormalizedWebhookEvent{}, fmt.Errorf("providers/meta/common: provider id is required")
	}

	deliveryID, err := webhooks.HeaderDeliveryIDExtractor("X-Meta-Delivery-Id", "X-Hub-Signature-256")(req)
	if err != nil {
		return NormalizedWebhookEvent{}, err
	}

	var payload rawWebhookEnvelope
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return NormalizedWebhookEvent{}, fmt.Errorf("providers/meta/common: parse webhook payload: %w", err)
	}
	object := strings.TrimSpace(strings.ToLower(payload.Object))
	if object == "" {
		return NormalizedWebhookEvent{}, fmt.Errorf("providers/meta/common: webhook object is required")
	}
	if err := validateObject(providerID, object); err != nil {
		return NormalizedWebhookEvent{}, err
	}

	fieldSet := map[string]struct{}{}
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			field := strings.TrimSpace(strings.ToLower(change.Field))
			if field == "" {
				continue
			}
			fieldSet[field] = struct{}{}
		}
	}
	changeFields := make([]string, 0, len(fieldSet))
	for field := range fieldSet {
		changeFields = append(changeFields, field)
	}
	sort.Strings(changeFields)

	return NormalizedWebhookEvent{
		ProviderID:   providerID,
		DeliveryID:   deliveryID,
		Object:       object,
		EntryCount:   len(payload.Entry),
		ChangeFields: changeFields,
	}, nil
}

func validateObject(providerID string, object string) error {
	allowedObjects := map[string]map[string]struct{}{
		providerIDMeta: {
			"instagram": {},
			"page":      {},
			"user":      {},
		},
		providerIDInstagram: {
			"instagram": {},
		},
		providerIDFacebook: {
			"page": {},
			"user": {},
		},
	}
	allowed, ok := allowedObjects[providerID]
	if !ok {
		return nil
	}
	if _, ok := allowed[object]; ok {
		return nil
	}
	return fmt.Errorf(
		"providers/meta/common: object %q is not supported for provider %q",
		object,
		providerID,
	)
}
