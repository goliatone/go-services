package core

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type inMemorySyncConflictStore struct {
	records map[string]SyncConflict
	nextID  int
}

func newInMemorySyncConflictStore() *inMemorySyncConflictStore {
	return &inMemorySyncConflictStore{
		records: make(map[string]SyncConflict),
		nextID:  1,
	}
}

func (s *inMemorySyncConflictStore) Append(
	ctx context.Context,
	conflict SyncConflict,
) (SyncConflict, error) {
	if conflict.ID == "" {
		conflict.ID = fmt.Sprintf("conflict_%d", s.nextID)
		s.nextID++
	}
	if conflict.Status == "" {
		conflict.Status = SyncConflictStatusPending
	}
	s.records[conflict.ID] = conflict
	return conflict, nil
}

func (s *inMemorySyncConflictStore) Get(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	id string,
) (SyncConflict, error) {
	conflict, ok := s.records[id]
	if !ok {
		return SyncConflict{}, fmt.Errorf("conflict not found")
	}
	if !sameProviderScope(conflict.ProviderID, conflict.Scope, providerID, scope) {
		return SyncConflict{}, fmt.Errorf("conflict not found")
	}
	return conflict, nil
}

func (s *inMemorySyncConflictStore) ListByBinding(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	syncBindingID string,
	status SyncConflictStatus,
) ([]SyncConflict, error) {
	out := make([]SyncConflict, 0)
	for _, conflict := range s.records {
		if !sameProviderScope(conflict.ProviderID, conflict.Scope, providerID, scope) {
			continue
		}
		if conflict.SyncBindingID != syncBindingID {
			continue
		}
		if status != "" && conflict.Status != status {
			continue
		}
		out = append(out, conflict)
	}
	return out, nil
}

func (s *inMemorySyncConflictStore) Resolve(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	id string,
	resolution SyncConflictResolution,
	resolvedAt time.Time,
) (SyncConflict, error) {
	conflict, err := s.Get(ctx, providerID, scope, id)
	if err != nil {
		return SyncConflict{}, err
	}
	switch resolution.Action {
	case SyncConflictResolutionResolve:
		conflict.Status = SyncConflictStatusResolved
	case SyncConflictResolutionIgnore:
		conflict.Status = SyncConflictStatusIgnored
	case SyncConflictResolutionRetry:
		conflict.Status = SyncConflictStatusPending
	}
	conflict.ResolvedBy = resolution.ResolvedBy
	conflict.ResolvedAt = &resolvedAt
	if conflict.Resolution == nil {
		conflict.Resolution = make(map[string]any)
	}
	for key, value := range resolution.Patch {
		conflict.Resolution[key] = value
	}
	if resolution.Reason != "" {
		conflict.Resolution["reason"] = resolution.Reason
	}
	s.records[id] = conflict
	return conflict, nil
}

type recordingLifecycleEventBus struct {
	events []LifecycleEvent
}

func (b *recordingLifecycleEventBus) Publish(ctx context.Context, event LifecycleEvent) error {
	b.events = append(b.events, event)
	return nil
}

func (b *recordingLifecycleEventBus) Subscribe(handler LifecycleEventHandler) {}

type defaultConflictPolicyHook struct{}

func (defaultConflictPolicyHook) ApplyRecordPolicy(
	ctx context.Context,
	conflict SyncConflict,
) (SyncConflict, error) {
	if conflict.Policy == "" {
		conflict.Policy = "manual_review"
	}
	if conflict.Metadata == nil {
		conflict.Metadata = make(map[string]any)
	}
	conflict.Metadata["policy.applied"] = true
	return conflict, nil
}

func (defaultConflictPolicyHook) ApplyResolutionPolicy(
	ctx context.Context,
	conflict SyncConflict,
	resolution SyncConflictResolution,
) (SyncConflictResolution, error) {
	if resolution.Reason == "" {
		resolution.Reason = "policy_default_reason"
	}
	return resolution, nil
}

func TestSyncConflictLedgerServiceRecordAndResolveWorkflow(t *testing.T) {
	store := newInMemorySyncConflictStore()
	eventBus := &recordingLifecycleEventBus{}
	service, err := NewSyncConflictLedgerService(
		store,
		WithSyncConflictEventBus(eventBus),
		WithSyncConflictPolicyHook(defaultConflictPolicyHook{}),
		WithSyncConflictClock(func() time.Time {
			return time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
		}),
	)
	if err != nil {
		t.Fatalf("new sync conflict ledger service: %v", err)
	}

	recorded, err := service.RecordSyncConflict(context.Background(), RecordSyncConflictRequest{
		Conflict: SyncConflict{
			ProviderID:    "hubspot",
			Scope:         ScopeRef{Type: "org", ID: "org_123"},
			ConnectionID:  "conn_1",
			SyncBindingID: "sync_binding_1",
			SourceObject:  "contacts",
			ExternalID:    "ext_1",
			Reason:        "email mismatch",
			SourcePayload: map[string]any{
				"email":         "a@example.com",
				"access_token":  "plain-secret-token",
				"provider_id":   "hubspot",
				"connection_id": "conn_1",
			},
		},
		Metadata: map[string]any{
			"authorization": "Bearer plain-secret-token",
			"trace_id":      "trace_1",
		},
	})
	if err != nil {
		t.Fatalf("record conflict: %v", err)
	}
	if recorded.Conflict.Status != SyncConflictStatusPending {
		t.Fatalf("expected pending status, got %s", recorded.Conflict.Status)
	}
	if recorded.Conflict.Policy != "manual_review" {
		t.Fatalf("expected policy hook to set policy, got %q", recorded.Conflict.Policy)
	}
	if recorded.Conflict.SourcePayload["access_token"] != RedactedValue {
		t.Fatalf("expected source payload token redaction, got %#v", recorded.Conflict.SourcePayload["access_token"])
	}
	if recorded.Conflict.SourcePayload["provider_id"] != "hubspot" {
		t.Fatalf("expected traceability provider_id to remain visible, got %#v", recorded.Conflict.SourcePayload["provider_id"])
	}
	if recorded.Conflict.Metadata["authorization"] != RedactedValue {
		t.Fatalf("expected metadata authorization to be redacted, got %#v", recorded.Conflict.Metadata["authorization"])
	}
	if recorded.Conflict.Metadata["trace_id"] != "trace_1" {
		t.Fatalf("expected metadata trace_id to remain visible, got %#v", recorded.Conflict.Metadata["trace_id"])
	}

	resolved, err := service.ResolveSyncConflict(context.Background(), ResolveSyncConflictRequest{
		ProviderID: "hubspot",
		Scope:      ScopeRef{Type: "org", ID: "org_123"},
		ConflictID: recorded.Conflict.ID,
		Resolution: SyncConflictResolution{
			Action:     SyncConflictResolutionResolve,
			Reason:     "manual fix",
			ResolvedBy: "user_1",
		},
	})
	if err != nil {
		t.Fatalf("resolve conflict: %v", err)
	}
	if resolved.Conflict.Status != SyncConflictStatusResolved {
		t.Fatalf("expected resolved status, got %s", resolved.Conflict.Status)
	}
	if resolved.Conflict.ResolvedBy != "user_1" {
		t.Fatalf("expected resolved_by to be user_1, got %q", resolved.Conflict.ResolvedBy)
	}

	if len(eventBus.events) != 2 {
		t.Fatalf("expected two audit events, got %d", len(eventBus.events))
	}
	if eventBus.events[0].Name != "services.sync.conflict.recorded" {
		t.Fatalf("expected first event to be recorded, got %q", eventBus.events[0].Name)
	}
	if eventBus.events[1].Name != "services.sync.conflict.resolved" {
		t.Fatalf("expected second event to be resolved, got %q", eventBus.events[1].Name)
	}
}

func TestSyncConflictLedgerServiceIgnoreWorkflow(t *testing.T) {
	store := newInMemorySyncConflictStore()
	eventBus := &recordingLifecycleEventBus{}
	service, err := NewSyncConflictLedgerService(
		store,
		WithSyncConflictEventBus(eventBus),
		WithSyncConflictPolicyHook(defaultConflictPolicyHook{}),
	)
	if err != nil {
		t.Fatalf("new sync conflict ledger service: %v", err)
	}

	recorded, err := service.RecordSyncConflict(context.Background(), RecordSyncConflictRequest{
		Conflict: SyncConflict{
			ProviderID:    "hubspot",
			Scope:         ScopeRef{Type: "org", ID: "org_123"},
			ConnectionID:  "conn_1",
			SyncBindingID: "sync_binding_1",
			SourceObject:  "contacts",
			ExternalID:    "ext_2",
			Reason:        "phone mismatch",
		},
	})
	if err != nil {
		t.Fatalf("record conflict: %v", err)
	}

	ignored, err := service.ResolveSyncConflict(context.Background(), ResolveSyncConflictRequest{
		ProviderID: "hubspot",
		Scope:      ScopeRef{Type: "org", ID: "org_123"},
		ConflictID: recorded.Conflict.ID,
		Resolution: SyncConflictResolution{
			Action:     SyncConflictResolutionIgnore,
			ResolvedBy: "user_2",
		},
	})
	if err != nil {
		t.Fatalf("ignore conflict: %v", err)
	}
	if ignored.Conflict.Status != SyncConflictStatusIgnored {
		t.Fatalf("expected ignored status, got %s", ignored.Conflict.Status)
	}

	if len(eventBus.events) != 2 {
		t.Fatalf("expected two audit events, got %d", len(eventBus.events))
	}
	if eventBus.events[1].Name != "services.sync.conflict.ignored" {
		t.Fatalf("expected ignored audit event, got %q", eventBus.events[1].Name)
	}
}

func TestSyncConflictLedgerServiceResolveFailsClosedOnScopeMismatch(t *testing.T) {
	store := newInMemorySyncConflictStore()
	service, err := NewSyncConflictLedgerService(store)
	if err != nil {
		t.Fatalf("new sync conflict ledger service: %v", err)
	}

	recorded, err := service.RecordSyncConflict(context.Background(), RecordSyncConflictRequest{
		Conflict: SyncConflict{
			ProviderID:    "hubspot",
			Scope:         ScopeRef{Type: "org", ID: "org_123"},
			ConnectionID:  "conn_1",
			SyncBindingID: "sync_binding_1",
			SourceObject:  "contacts",
			ExternalID:    "ext_3",
			Reason:        "name mismatch",
		},
	})
	if err != nil {
		t.Fatalf("record conflict: %v", err)
	}

	_, err = service.ResolveSyncConflict(context.Background(), ResolveSyncConflictRequest{
		ProviderID: "hubspot",
		Scope:      ScopeRef{Type: "org", ID: "org_999"},
		ConflictID: recorded.Conflict.ID,
		Resolution: SyncConflictResolution{
			Action:     SyncConflictResolutionResolve,
			ResolvedBy: "user_3",
		},
	})
	if err == nil {
		t.Fatalf("expected resolve conflict to fail for mismatched scope")
	}
}
