package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestOrchestrator_StartBootstrapUsesStoredCursorCheckpoint(t *testing.T) {
	jobStore := newMemorySyncJobStore()
	cursorStore := &stubSyncCursorStore{
		cursor: core.SyncCursor{
			ConnectionID: "conn_1",
			ProviderID:   "github",
			ResourceType: "drive.file",
			ResourceID:   "file_1",
			Cursor:       "checkpoint_1",
		},
	}
	orchestrator := NewOrchestrator(jobStore, cursorStore)

	job, err := orchestrator.StartBootstrap(context.Background(), core.BootstrapRequest{
		ConnectionID: "conn_1",
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_1",
		Metadata: map[string]any{
			"source": "webhook",
		},
	})
	if err != nil {
		t.Fatalf("start bootstrap: %v", err)
	}
	if job.Mode != core.SyncJobModeBootstrap {
		t.Fatalf("expected bootstrap mode")
	}
	if job.Checkpoint != "checkpoint_1" {
		t.Fatalf("expected checkpoint from stored cursor")
	}
	if job.Metadata["source"] != "webhook" {
		t.Fatalf("expected metadata to persist")
	}
}

func TestOrchestrator_DurableCheckpointAndResume(t *testing.T) {
	jobStore := newMemorySyncJobStore()
	orchestrator := NewOrchestrator(jobStore, &stubSyncCursorStore{})

	job, err := orchestrator.StartIncremental(
		context.Background(),
		"conn_2",
		"github",
		"drive.file",
		"file_2",
		map[string]any{"reason": "webhook"},
	)
	if err != nil {
		t.Fatalf("start incremental: %v", err)
	}

	checkpointed, err := orchestrator.SaveCheckpoint(
		context.Background(),
		job.ID,
		"cursor_2",
		map[string]any{"page": 2},
	)
	if err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	if checkpointed.Status != core.SyncJobStatusRunning {
		t.Fatalf("expected running status after checkpoint")
	}
	if checkpointed.Checkpoint != "cursor_2" {
		t.Fatalf("expected checkpoint to persist")
	}

	nextAttempt := time.Now().UTC().Add(2 * time.Minute)
	failed, err := orchestrator.Fail(context.Background(), job.ID, errors.New("transient"), &nextAttempt)
	if err != nil {
		t.Fatalf("fail job: %v", err)
	}
	if failed.Status != core.SyncJobStatusFailed {
		t.Fatalf("expected failed status")
	}
	if failed.NextAttemptAt == nil {
		t.Fatalf("expected next attempt timestamp")
	}

	if err := orchestrator.Resume(context.Background(), job.ID); err != nil {
		t.Fatalf("resume job: %v", err)
	}
	resumed, err := jobStore.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("load resumed job: %v", err)
	}
	if resumed.Status != core.SyncJobStatusQueued {
		t.Fatalf("expected queued status after resume")
	}
	if resumed.Checkpoint != "cursor_2" {
		t.Fatalf("expected checkpoint to remain durable across resume")
	}
}

type memorySyncJobStore struct {
	records map[string]core.SyncJob
}

func newMemorySyncJobStore() *memorySyncJobStore {
	return &memorySyncJobStore{records: map[string]core.SyncJob{}}
}

func (s *memorySyncJobStore) Create(_ context.Context, job core.SyncJob) (core.SyncJob, error) {
	s.records[job.ID] = job
	return job, nil
}

func (s *memorySyncJobStore) Get(_ context.Context, id string) (core.SyncJob, error) {
	job, ok := s.records[id]
	if !ok {
		return core.SyncJob{}, errors.New("missing job")
	}
	return job, nil
}

func (s *memorySyncJobStore) Update(_ context.Context, job core.SyncJob) (core.SyncJob, error) {
	s.records[job.ID] = job
	return job, nil
}

type stubSyncCursorStore struct {
	cursor core.SyncCursor
	err    error
}

func (s *stubSyncCursorStore) Get(_ context.Context, _ string, _ string, _ string) (core.SyncCursor, error) {
	if s.err != nil {
		return core.SyncCursor{}, s.err
	}
	if s.cursor.Cursor == "" {
		return core.SyncCursor{}, errors.New("missing cursor")
	}
	return s.cursor, nil
}

func (*stubSyncCursorStore) Upsert(context.Context, core.UpsertSyncCursorInput) (core.SyncCursor, error) {
	return core.SyncCursor{}, nil
}

func (*stubSyncCursorStore) Advance(context.Context, core.AdvanceSyncCursorInput) (core.SyncCursor, error) {
	return core.SyncCursor{}, nil
}
