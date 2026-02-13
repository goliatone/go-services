package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultLifecycleChannel = "services.lifecycle"
	DefaultProjectorName    = "go-notifications"
)

type LifecycleProjectorRegistry struct {
	mu       sync.RWMutex
	handlers map[string]LifecycleEventHandler
	order    []string
}

func NewLifecycleProjectorRegistry() *LifecycleProjectorRegistry {
	return &LifecycleProjectorRegistry{
		handlers: make(map[string]LifecycleEventHandler),
		order:    make([]string, 0),
	}
}

func (r *LifecycleProjectorRegistry) Register(name string, handler LifecycleEventHandler) {
	if r == nil || handler == nil {
		return
	}
	key := strings.TrimSpace(name)
	if key == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = make(map[string]LifecycleEventHandler)
	}
	if _, exists := r.handlers[key]; !exists {
		r.order = append(r.order, key)
		sort.Strings(r.order)
	}
	r.handlers[key] = handler
}

func (r *LifecycleProjectorRegistry) Handlers() []LifecycleEventHandler {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]LifecycleEventHandler, 0, len(r.order))
	for _, key := range r.order {
		handler := r.handlers[key]
		if handler != nil {
			out = append(out, handler)
		}
	}
	return out
}

type LifecycleActivityProjector struct {
	sink     ServicesActivitySink
	enricher ServicesActivityEnricher
	now      func() time.Time
}

func NewLifecycleActivityProjector(
	sink ServicesActivitySink,
	enricher ServicesActivityEnricher,
) *LifecycleActivityProjector {
	return &LifecycleActivityProjector{
		sink:     sink,
		enricher: enricher,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (p *LifecycleActivityProjector) Handle(ctx context.Context, event LifecycleEvent) error {
	if p == nil || p.sink == nil {
		return fmt.Errorf("core: activity sink is required")
	}
	entry := ServiceActivityEntry{
		ID:        strings.TrimSpace(event.ID),
		Actor:     activityActor(event),
		Action:    strings.TrimSpace(event.Name),
		Object:    activityObject(event),
		Channel:   DefaultLifecycleChannel,
		Status:    activityStatus(event),
		Metadata:  activityMetadata(event),
		CreatedAt: activityTime(event, p.now),
	}
	if p.enricher != nil {
		enriched, err := p.enricher.Enrich(ctx, entry)
		if err != nil {
			return err
		}
		entry = enriched
	}
	return p.sink.Record(ctx, entry)
}

type GoNotificationsProjector struct {
	ProjectorName      string
	DefinitionResolver NotificationDefinitionResolver
	RecipientResolver  NotificationRecipientResolver
	Sender             NotificationSender
	Ledger             NotificationDispatchLedger
}

func NewGoNotificationsProjector(
	definitionResolver NotificationDefinitionResolver,
	recipientResolver NotificationRecipientResolver,
	sender NotificationSender,
	ledger NotificationDispatchLedger,
) *GoNotificationsProjector {
	return &GoNotificationsProjector{
		ProjectorName:      DefaultProjectorName,
		DefinitionResolver: definitionResolver,
		RecipientResolver:  recipientResolver,
		Sender:             sender,
		Ledger:             ledger,
	}
}

func (p *GoNotificationsProjector) Handle(ctx context.Context, event LifecycleEvent) error {
	if p == nil || p.DefinitionResolver == nil || p.RecipientResolver == nil || p.Sender == nil || p.Ledger == nil {
		return fmt.Errorf("core: go-notifications projector dependencies are required")
	}
	definitionCode, ok, err := p.DefinitionResolver.Resolve(ctx, event)
	if err != nil {
		return err
	}
	definitionCode = strings.TrimSpace(definitionCode)
	if !ok || definitionCode == "" {
		return nil
	}

	recipients, err := p.RecipientResolver.Resolve(ctx, event)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return nil
	}

	projectorName := strings.TrimSpace(p.ProjectorName)
	if projectorName == "" {
		projectorName = DefaultProjectorName
	}

	for _, recipient := range recipients {
		recipientKey := formatRecipientKey(recipient)
		if recipientKey == "" {
			continue
		}
		idempotencyKey := buildDispatchID(projectorName, definitionCode, event, recipientKey)
		seen, err := p.Ledger.Seen(ctx, idempotencyKey)
		if err != nil {
			return err
		}
		if seen {
			continue
		}

		sendErr := p.Sender.Send(ctx, NotificationSendRequest{
			DefinitionCode: definitionCode,
			Recipients:     []Recipient{recipient},
			Event:          event,
			Metadata:       copyMap(event.Metadata),
		})
		record := NotificationDispatchRecord{
			EventID:        strings.TrimSpace(event.ID),
			Projector:      projectorName,
			DefinitionCode: definitionCode,
			RecipientKey:   recipientKey,
			IdempotencyKey: idempotencyKey,
			Status:         "sent",
			Error:          "",
			Metadata:       copyMap(event.Metadata),
		}
		if sendErr != nil {
			record.Status = "failed"
			record.Error = sendErr.Error()
		}
		if err := p.Ledger.Record(ctx, record); err != nil {
			return err
		}
		if sendErr != nil {
			return sendErr
		}
	}

	return nil
}

func activityActor(event LifecycleEvent) string {
	actor := strings.TrimSpace(event.Source)
	if actor == "" {
		return "system"
	}
	return actor
}

func activityObject(event LifecycleEvent) string {
	if strings.TrimSpace(event.ConnectionID) != "" {
		return "connection:" + strings.TrimSpace(event.ConnectionID)
	}
	return "provider:" + strings.TrimSpace(event.ProviderID)
}

func activityTime(event LifecycleEvent, nowFn func() time.Time) time.Time {
	if !event.OccurredAt.IsZero() {
		return event.OccurredAt.UTC()
	}
	if nowFn == nil {
		return time.Now().UTC()
	}
	return nowFn().UTC()
}

func activityStatus(event LifecycleEvent) ServiceActivityStatus {
	if raw, ok := event.Metadata["status"]; ok {
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(raw))) {
		case string(ServiceActivityStatusError):
			return ServiceActivityStatusError
		case string(ServiceActivityStatusWarn):
			return ServiceActivityStatusWarn
		}
	}
	name := strings.ToLower(strings.TrimSpace(event.Name))
	if strings.Contains(name, "fail") || strings.Contains(name, "error") {
		return ServiceActivityStatusError
	}
	if strings.Contains(name, "retry") || strings.Contains(name, "degrad") {
		return ServiceActivityStatusWarn
	}
	return ServiceActivityStatusOK
}

func activityMetadata(event LifecycleEvent) map[string]any {
	metadata := copyMap(event.Metadata)
	metadata["provider_id"] = strings.TrimSpace(event.ProviderID)
	metadata["scope_type"] = strings.TrimSpace(event.ScopeType)
	metadata["scope_id"] = strings.TrimSpace(event.ScopeID)
	metadata["connection_id"] = strings.TrimSpace(event.ConnectionID)
	metadata["event_name"] = strings.TrimSpace(event.Name)
	if len(event.Payload) > 0 {
		metadata["payload"] = copyMap(event.Payload)
	}
	return metadata
}

func formatRecipientKey(recipient Recipient) string {
	id := strings.TrimSpace(recipient.ID)
	if id == "" {
		return ""
	}
	kind := strings.TrimSpace(recipient.Type)
	if kind == "" {
		return id
	}
	return strings.ToLower(kind) + ":" + id
}

func buildDispatchID(projectorName string, definitionCode string, event LifecycleEvent, recipientKey string) string {
	eventID := strings.TrimSpace(event.ID)
	if eventID == "" {
		eventID = strings.TrimSpace(event.Name) + "|" + event.OccurredAt.UTC().Format(time.RFC3339Nano)
	}
	raw := strings.Join([]string{
		strings.TrimSpace(projectorName),
		strings.TrimSpace(definitionCode),
		eventID,
		strings.TrimSpace(recipientKey),
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func copyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

var (
	_ ProjectorRegistry     = (*LifecycleProjectorRegistry)(nil)
	_ ActivityProjector     = (*LifecycleActivityProjector)(nil)
	_ NotificationProjector = (*GoNotificationsProjector)(nil)
)
