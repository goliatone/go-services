package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type inMemorySyncCheckpointStore struct {
	saveCount int
	latest    map[string]SyncCheckpoint
	byID      map[string]SyncCheckpoint
}

func newInMemorySyncCheckpointStore() *inMemorySyncCheckpointStore {
	return &inMemorySyncCheckpointStore{
		latest: make(map[string]SyncCheckpoint),
		byID:   make(map[string]SyncCheckpoint),
	}
}

func (s *inMemorySyncCheckpointStore) Save(
	ctx context.Context,
	checkpoint SyncCheckpoint,
) (SyncCheckpoint, error) {
	s.saveCount++
	if checkpoint.ID == "" {
		checkpoint.ID = fmt.Sprintf("checkpoint_%d", s.saveCount)
	}
	key := checkpointStoreKey(
		checkpoint.ProviderID,
		checkpoint.Scope,
		checkpoint.SyncBindingID,
		checkpoint.Direction,
	)
	s.latest[key] = checkpoint
	s.byID[checkpoint.ID] = checkpoint
	return checkpoint, nil
}

func (s *inMemorySyncCheckpointStore) GetByID(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	id string,
) (SyncCheckpoint, bool, error) {
	checkpoint, ok := s.byID[id]
	if ok && !sameProviderScope(checkpoint.ProviderID, checkpoint.Scope, providerID, scope) {
		return SyncCheckpoint{}, false, nil
	}
	return checkpoint, ok, nil
}

func (s *inMemorySyncCheckpointStore) GetLatest(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	syncBindingID string,
	direction SyncDirection,
) (SyncCheckpoint, bool, error) {
	key := checkpointStoreKey(providerID, scope, syncBindingID, direction)
	checkpoint, ok := s.latest[key]
	return checkpoint, ok, nil
}

func checkpointStoreKey(
	providerID string,
	scope ScopeRef,
	syncBindingID string,
	direction SyncDirection,
) string {
	return strings.ToLower(strings.TrimSpace(providerID)) +
		"::" +
		strings.ToLower(strings.TrimSpace(scope.Type)) +
		"::" +
		strings.TrimSpace(scope.ID) +
		"::" +
		strings.TrimSpace(syncBindingID) +
		"::" +
		string(direction)
}

type inMemorySyncChangeLogStore struct {
	entries map[string]SyncChangeLogEntry
}

func newInMemorySyncChangeLogStore() *inMemorySyncChangeLogStore {
	return &inMemorySyncChangeLogStore{
		entries: make(map[string]SyncChangeLogEntry),
	}
}

func (s *inMemorySyncChangeLogStore) Append(
	ctx context.Context,
	entry SyncChangeLogEntry,
) (bool, error) {
	if _, exists := s.entries[entry.IdempotencyKey]; exists {
		return false, nil
	}
	s.entries[entry.IdempotencyKey] = entry
	return true, nil
}

func (s *inMemorySyncChangeLogStore) ListSince(
	ctx context.Context,
	syncBindingID string,
	direction SyncDirection,
	cursor string,
	limit int,
) ([]SyncChangeLogEntry, string, error) {
	out := make([]SyncChangeLogEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry.SyncBindingID == syncBindingID && entry.Direction == direction {
			out = append(out, entry)
		}
	}
	return out, "", nil
}

func TestSyncExecutionServiceRunSyncImportSourceVersionAwareIdempotency(t *testing.T) {
	checkpointStore := newInMemorySyncCheckpointStore()
	changeLogStore := newInMemorySyncChangeLogStore()
	service, err := NewSyncExecutionService(
		checkpointStore,
		changeLogStore,
		WithSyncExecutionClock(func() time.Time {
			return time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
		}),
	)
	if err != nil {
		t.Fatalf("new sync execution service: %v", err)
	}

	request := RunSyncImportRequest{
		Plan: SyncRunPlan{
			ID:        "run_import_1",
			BindingID: "sync_binding_1",
			Mode:      SyncRunModeApply,
			Checkpoint: SyncCheckpoint{
				ProviderID:   "hubspot",
				Scope:        ScopeRef{Type: "org", ID: "org_123"},
				ConnectionID: "conn_1",
				Direction:    SyncDirectionImport,
			},
		},
		Changes: []SyncChange{
			{
				SourceObject:  "contacts",
				ExternalID:    "ext_1",
				SourceVersion: "v1",
				Payload: map[string]any{
					"email":        "a@example.com",
					"access_token": "secret-token-v1",
				},
				Metadata: map[string]any{
					"authorization": "Bearer secret-token-v1",
					"trace_id":      "trace_1",
				},
			},
			{
				SourceObject:  "contacts",
				ExternalID:    "ext_1",
				SourceVersion: "v2",
				Payload: map[string]any{
					"email":         "b@example.com",
					"refresh_token": "secret-refresh-v2",
				},
			},
		},
	}

	first, err := service.RunSyncImport(context.Background(), request)
	if err != nil {
		t.Fatalf("run sync import first: %v", err)
	}
	if first.Status != SyncRunStatusSucceeded {
		t.Fatalf("expected succeeded status, got %s", first.Status)
	}
	if first.ProcessedCount != 2 || first.SkippedCount != 0 || first.FailedCount != 0 {
		t.Fatalf("unexpected first run counters: %#v", first)
	}
	if first.NextCheckpoint == nil || first.NextCheckpoint.Sequence != 2 {
		t.Fatalf("expected checkpoint sequence=2, got %#v", first.NextCheckpoint)
	}

	second, err := service.RunSyncImport(context.Background(), request)
	if err != nil {
		t.Fatalf("run sync import second: %v", err)
	}
	if second.Status != SyncRunStatusSucceeded {
		t.Fatalf("expected succeeded status, got %s", second.Status)
	}
	if second.ProcessedCount != 0 || second.SkippedCount != 2 || second.FailedCount != 0 {
		t.Fatalf("unexpected second run counters: %#v", second)
	}
	if second.NextCheckpoint == nil || second.NextCheckpoint.Sequence != 2 {
		t.Fatalf("expected second checkpoint sequence=2, got %#v", second.NextCheckpoint)
	}
	if len(changeLogStore.entries) != 2 {
		t.Fatalf("expected exactly two unique changelog entries, got %d", len(changeLogStore.entries))
	}
	for _, entry := range changeLogStore.entries {
		if token, ok := entry.Payload["access_token"]; ok && token != RedactedValue {
			t.Fatalf("expected access_token redaction, got %#v", token)
		}
		if token, ok := entry.Payload["refresh_token"]; ok && token != RedactedValue {
			t.Fatalf("expected refresh_token redaction, got %#v", token)
		}
		if entry.Metadata["authorization"] == "Bearer secret-token-v1" {
			t.Fatalf("expected authorization metadata to be redacted")
		}
		if traceID, ok := entry.Metadata["trace_id"]; ok && traceID != "trace_1" {
			t.Fatalf("expected trace_id metadata to remain visible, got %#v", traceID)
		}
	}
	if checkpointStore.saveCount != 4 {
		t.Fatalf("expected four checkpoint saves across two runs, got %d", checkpointStore.saveCount)
	}
}

func TestSyncExecutionServiceRunSyncDirectionSpecificIdempotencyKeys(t *testing.T) {
	checkpointStore := newInMemorySyncCheckpointStore()
	changeLogStore := newInMemorySyncChangeLogStore()
	service, err := NewSyncExecutionService(checkpointStore, changeLogStore)
	if err != nil {
		t.Fatalf("new sync execution service: %v", err)
	}

	importReq := RunSyncImportRequest{
		Plan: SyncRunPlan{
			ID:        "run_import_1",
			BindingID: "sync_binding_2",
			Mode:      SyncRunModeApply,
			Checkpoint: SyncCheckpoint{
				ProviderID:   "hubspot",
				Scope:        ScopeRef{Type: "org", ID: "org_123"},
				ConnectionID: "conn_1",
				Direction:    SyncDirectionImport,
			},
		},
		Changes: []SyncChange{
			{SourceObject: "contacts", ExternalID: "ext_1", SourceVersion: "v1", Payload: map[string]any{"email": "a@example.com"}},
		},
	}
	if _, err := service.RunSyncImport(context.Background(), importReq); err != nil {
		t.Fatalf("run sync import: %v", err)
	}

	exportReq := RunSyncExportRequest{
		Plan: SyncRunPlan{
			ID:        "run_export_1",
			BindingID: "sync_binding_2",
			Mode:      SyncRunModeApply,
			Checkpoint: SyncCheckpoint{
				ProviderID:   "hubspot",
				Scope:        ScopeRef{Type: "org", ID: "org_123"},
				ConnectionID: "conn_1",
				Direction:    SyncDirectionExport,
			},
		},
		Changes: []SyncChange{
			{SourceObject: "contacts", ExternalID: "ext_1", SourceVersion: "v1", Payload: map[string]any{"email": "a@example.com"}},
		},
	}
	if _, err := service.RunSyncExport(context.Background(), exportReq); err != nil {
		t.Fatalf("run sync export: %v", err)
	}

	if len(changeLogStore.entries) != 2 {
		t.Fatalf("expected import/export to produce distinct idempotency entries, got %d", len(changeLogStore.entries))
	}
}

func TestSyncExecutionServiceEmitsLifecycleEvents(t *testing.T) {
	checkpointStore := newInMemorySyncCheckpointStore()
	changeLogStore := newInMemorySyncChangeLogStore()
	eventBus := &recordingLifecycleEventBus{}
	service, err := NewSyncExecutionService(
		checkpointStore,
		changeLogStore,
		WithSyncExecutionEventBus(eventBus),
		WithSyncExecutionClock(func() time.Time {
			return time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
		}),
	)
	if err != nil {
		t.Fatalf("new sync execution service: %v", err)
	}

	_, err = service.RunSyncImport(context.Background(), RunSyncImportRequest{
		Plan: SyncRunPlan{
			ID:        "run_import_events",
			BindingID: "sync_binding_events",
			Mode:      SyncRunModeApply,
			Checkpoint: SyncCheckpoint{
				ProviderID:    "hubspot",
				Scope:         ScopeRef{Type: "org", ID: "org_123"},
				ConnectionID:  "conn_1",
				SyncBindingID: "sync_binding_events",
				Direction:     SyncDirectionImport,
			},
		},
		Changes: []SyncChange{
			{SourceObject: "contacts", ExternalID: "ext_1", SourceVersion: "v1", Payload: map[string]any{"email": "a@example.com"}},
		},
	})
	if err != nil {
		t.Fatalf("run sync import: %v", err)
	}

	if len(eventBus.events) != 3 {
		t.Fatalf("expected 3 lifecycle events, got %d", len(eventBus.events))
	}
	if eventBus.events[0].Name != "services.sync.run.started" {
		t.Fatalf("unexpected first event name %q", eventBus.events[0].Name)
	}
	if eventBus.events[1].Name != "services.sync.run.checkpoint" {
		t.Fatalf("unexpected second event name %q", eventBus.events[1].Name)
	}
	if eventBus.events[2].Name != "services.sync.run.succeeded" {
		t.Fatalf("unexpected third event name %q", eventBus.events[2].Name)
	}
}
