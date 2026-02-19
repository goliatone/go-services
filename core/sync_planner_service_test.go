package core

import (
	"context"
	"testing"
	"time"
)

func TestSyncPlannerServicePlanSyncRunDeterministicOutput(t *testing.T) {
	checkpointStore := newInMemorySyncCheckpointStore()
	_, err := checkpointStore.Save(context.Background(), SyncCheckpoint{
		ProviderID:    "hubspot",
		Scope:         ScopeRef{Type: "org", ID: "org_123"},
		ConnectionID:  "conn_1",
		SyncBindingID: "sync_binding_1",
		Direction:     SyncDirectionImport,
		Cursor:        "cursor_v3",
		Sequence:      3,
		SourceVersion: "v3",
	})
	if err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	planner, err := NewSyncPlannerService(
		checkpointStore,
		WithSyncPlannerClock(func() time.Time {
			return time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
		}),
	)
	if err != nil {
		t.Fatalf("new sync planner service: %v", err)
	}

	request := PlanSyncRunRequest{
		Binding: SyncBinding{
			ID:            "sync_binding_1",
			ProviderID:    "hubspot",
			Scope:         ScopeRef{Type: "org", ID: "org_123"},
			ConnectionID:  "conn_1",
			MappingSpecID: "spec_1",
			SourceObject:  "contacts",
			TargetModel:   "crm_contacts",
			Direction:     SyncDirectionImport,
			Status:        SyncBindingStatusActive,
		},
		Mode:     SyncRunModeApply,
		Limit:    200,
		Metadata: map[string]any{"tenant": "org_123"},
	}

	first, err := planner.PlanSyncRun(context.Background(), request)
	if err != nil {
		t.Fatalf("plan sync run first: %v", err)
	}
	second, err := planner.PlanSyncRun(context.Background(), request)
	if err != nil {
		t.Fatalf("plan sync run second: %v", err)
	}

	if first.DeterministicHash == "" {
		t.Fatalf("expected deterministic hash")
	}
	if first.DeterministicHash != second.DeterministicHash {
		t.Fatalf("expected deterministic hash equality, got %q and %q", first.DeterministicHash, second.DeterministicHash)
	}
	if first.ID != second.ID {
		t.Fatalf("expected deterministic plan id equality, got %q and %q", first.ID, second.ID)
	}
	if first.Checkpoint.Sequence != 3 || first.Checkpoint.Cursor != "cursor_v3" {
		t.Fatalf("expected latest checkpoint to be reused, got %#v", first.Checkpoint)
	}
}

func TestSyncPlannerServicePlanSyncRunFromCheckpointValidation(t *testing.T) {
	checkpointStore := newInMemorySyncCheckpointStore()
	other, err := checkpointStore.Save(context.Background(), SyncCheckpoint{
		ProviderID:    "hubspot",
		Scope:         ScopeRef{Type: "org", ID: "org_123"},
		ConnectionID:  "conn_1",
		SyncBindingID: "sync_binding_other",
		Direction:     SyncDirectionImport,
		Cursor:        "cursor_other",
		Sequence:      1,
	})
	if err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	planner, err := NewSyncPlannerService(checkpointStore)
	if err != nil {
		t.Fatalf("new sync planner service: %v", err)
	}

	_, err = planner.PlanSyncRun(context.Background(), PlanSyncRunRequest{
		Binding: SyncBinding{
			ID:            "sync_binding_1",
			ProviderID:    "hubspot",
			Scope:         ScopeRef{Type: "org", ID: "org_123"},
			ConnectionID:  "conn_1",
			MappingSpecID: "spec_1",
			SourceObject:  "contacts",
			TargetModel:   "crm_contacts",
			Direction:     SyncDirectionImport,
			Status:        SyncBindingStatusActive,
		},
		Mode:             SyncRunModeApply,
		FromCheckpointID: other.ID,
	})
	if err == nil {
		t.Fatalf("expected planning to fail when from-checkpoint binding differs")
	}
}

func TestSyncPlannerServicePlanSyncRunFailsClosedOnCheckpointScopeMismatch(t *testing.T) {
	checkpointStore := newInMemorySyncCheckpointStore()
	other, err := checkpointStore.Save(context.Background(), SyncCheckpoint{
		ProviderID:    "hubspot",
		Scope:         ScopeRef{Type: "org", ID: "org_999"},
		ConnectionID:  "conn_1",
		SyncBindingID: "sync_binding_1",
		Direction:     SyncDirectionImport,
		Cursor:        "cursor_other_scope",
		Sequence:      1,
	})
	if err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	planner, err := NewSyncPlannerService(checkpointStore)
	if err != nil {
		t.Fatalf("new sync planner service: %v", err)
	}

	_, err = planner.PlanSyncRun(context.Background(), PlanSyncRunRequest{
		Binding: SyncBinding{
			ID:            "sync_binding_1",
			ProviderID:    "hubspot",
			Scope:         ScopeRef{Type: "org", ID: "org_123"},
			ConnectionID:  "conn_1",
			MappingSpecID: "spec_1",
			SourceObject:  "contacts",
			TargetModel:   "crm_contacts",
			Direction:     SyncDirectionImport,
			Status:        SyncBindingStatusActive,
		},
		Mode:             SyncRunModeApply,
		FromCheckpointID: other.ID,
	})
	if err == nil {
		t.Fatalf("expected planning to fail when from-checkpoint scope differs")
	}
}
