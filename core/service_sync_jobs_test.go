package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestService_CreateSyncJob_ValidatesModeAndScope(t *testing.T) {
	ctx := context.Background()
	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "org", ID: "org_1"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	syncJobStore := newMemoryServiceSyncJobStore()
	svc, err := NewService(
		Config{},
		WithConnectionStore(connectionStore),
		WithSyncJobStore(syncJobStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.CreateSyncJob(ctx, CreateSyncJobRequest{
		ProviderID:   "github",
		ScopeType:    "org",
		ScopeID:      "org_1",
		ConnectionID: connection.ID,
		Mode:         SyncJobModeBootstrap,
	})
	if !strings.Contains(strings.ToLower(fmt.Sprint(err)), "invalid sync job mode") {
		t.Fatalf("expected invalid mode error, got %v", err)
	}

	_, err = svc.CreateSyncJob(ctx, CreateSyncJobRequest{
		ProviderID:   "github",
		ScopeType:    "team",
		ScopeID:      "org_1",
		ConnectionID: connection.ID,
		Mode:         SyncJobModeFull,
	})
	if err == nil || !strings.Contains(strings.ToLower(fmt.Sprint(err)), "invalid sync job scope") {
		t.Fatalf("expected invalid scope error, got %v", err)
	}
}

func TestService_CreateSyncJob_IdempotencyReplayAndConnectionFallback(t *testing.T) {
	ctx := context.Background()
	connectionStore := newMemoryConnectionStore()
	_, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "org", ID: "org_1"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	syncJobStore := newMemoryServiceSyncJobStore()
	svc, err := NewService(
		Config{},
		WithConnectionStore(connectionStore),
		WithSyncJobStore(syncJobStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	first, err := svc.CreateSyncJob(ctx, CreateSyncJobRequest{
		ProviderID:     "github",
		ScopeType:      "org",
		ScopeID:        "org_1",
		Mode:           SyncJobModeFull,
		IdempotencyKey: "idem_1",
		RequestedBy:    "wizard",
	})
	if err != nil {
		t.Fatalf("create sync job first: %v", err)
	}
	if !first.Created {
		t.Fatalf("expected first create to set Created=true")
	}
	if first.Job.ID == "" {
		t.Fatalf("expected sync job id")
	}
	if first.Job.Status != SyncJobStatusQueued {
		t.Fatalf("expected queued status, got %q", first.Job.Status)
	}

	replay, err := svc.CreateSyncJob(ctx, CreateSyncJobRequest{
		ProviderID:     "github",
		ScopeType:      "org",
		ScopeID:        "org_1",
		Mode:           SyncJobModeFull,
		IdempotencyKey: "idem_1",
	})
	if err != nil {
		t.Fatalf("create sync job replay: %v", err)
	}
	if replay.Created {
		t.Fatalf("expected replay to set Created=false")
	}
	if replay.Job.ID != first.Job.ID {
		t.Fatalf("expected replay job id %q, got %q", first.Job.ID, replay.Job.ID)
	}

	otherKey, err := svc.CreateSyncJob(ctx, CreateSyncJobRequest{
		ProviderID:     "github",
		ScopeType:      "org",
		ScopeID:        "org_1",
		Mode:           SyncJobModeFull,
		IdempotencyKey: "idem_2",
	})
	if err != nil {
		t.Fatalf("create sync job with different key: %v", err)
	}
	if !otherKey.Created {
		t.Fatalf("expected different key to create a new job")
	}
	if otherKey.Job.ID == first.Job.ID {
		t.Fatalf("expected different key to produce different job id")
	}

	noKeyA, err := svc.CreateSyncJob(ctx, CreateSyncJobRequest{
		ProviderID: "github",
		ScopeType:  "org",
		ScopeID:    "org_1",
		Mode:       SyncJobModeDelta,
	})
	if err != nil {
		t.Fatalf("create sync job without key first: %v", err)
	}
	noKeyB, err := svc.CreateSyncJob(ctx, CreateSyncJobRequest{
		ProviderID: "github",
		ScopeType:  "org",
		ScopeID:    "org_1",
		Mode:       SyncJobModeDelta,
	})
	if err != nil {
		t.Fatalf("create sync job without key second: %v", err)
	}
	if noKeyA.Job.ID == noKeyB.Job.ID {
		t.Fatalf("expected empty-key creates to produce distinct job ids")
	}
}

func TestService_GetSyncJob_GuardsAndNotFound(t *testing.T) {
	ctx := context.Background()
	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "org", ID: "org_1"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	syncJobStore := newMemoryServiceSyncJobStore()
	svc, err := NewService(
		Config{},
		WithConnectionStore(connectionStore),
		WithSyncJobStore(syncJobStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	created, err := svc.CreateSyncJob(ctx, CreateSyncJobRequest{
		ProviderID:   "github",
		ScopeType:    "org",
		ScopeID:      "org_1",
		ConnectionID: connection.ID,
		Mode:         SyncJobModeFull,
	})
	if err != nil {
		t.Fatalf("create sync job: %v", err)
	}

	loaded, err := svc.GetSyncJob(ctx, GetSyncJobRequest{
		SyncJobID:  created.Job.ID,
		ProviderID: "github",
		ScopeType:  "org",
		ScopeID:    "org_1",
	})
	if err != nil {
		t.Fatalf("get sync job: %v", err)
	}
	if loaded.ID != created.Job.ID {
		t.Fatalf("expected loaded sync job id %q, got %q", created.Job.ID, loaded.ID)
	}

	_, err = svc.GetSyncJob(ctx, GetSyncJobRequest{
		SyncJobID:  created.Job.ID,
		ProviderID: "slack",
	})
	if err == nil || !strings.Contains(strings.ToLower(fmt.Sprint(err)), "sync job not found") {
		t.Fatalf("expected provider guard miss to return sync job not found, got %v", err)
	}

	_, err = svc.GetSyncJob(ctx, GetSyncJobRequest{SyncJobID: "missing"})
	if err == nil || !strings.Contains(strings.ToLower(fmt.Sprint(err)), "sync job not found") {
		t.Fatalf("expected missing job not found error, got %v", err)
	}

	_, err = svc.GetSyncJob(ctx, GetSyncJobRequest{
		SyncJobID: created.Job.ID,
		ScopeType: "org",
		ScopeID:   "org_2",
	})
	if err == nil || !strings.Contains(strings.ToLower(fmt.Sprint(err)), "sync job not found") {
		t.Fatalf("expected scope guard miss to return sync job not found, got %v", err)
	}
}

type memoryServiceSyncJobStore struct {
	mu      sync.Mutex
	next    int
	jobs    map[string]SyncJob
	idemRef map[string]string
}

func newMemoryServiceSyncJobStore() *memoryServiceSyncJobStore {
	return &memoryServiceSyncJobStore{
		jobs:    map[string]SyncJob{},
		idemRef: map[string]string{},
	}
}

func (s *memoryServiceSyncJobStore) CreateSyncJob(
	_ context.Context,
	in CreateSyncJobStoreInput,
) (CreateSyncJobResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(in.ConnectionID) == "" {
		return CreateSyncJobResult{}, fmt.Errorf("connection id is required")
	}
	now := time.Now().UTC()
	newJob := func() SyncJob {
		s.next++
		id := fmt.Sprintf("job_%d", s.next)
		job := SyncJob{
			ID:           id,
			ConnectionID: in.ConnectionID,
			ProviderID:   in.ProviderID,
			Mode:         in.Mode,
			Status:       SyncJobStatusQueued,
			Metadata:     copyAnyMap(in.Metadata),
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		s.jobs[id] = job
		return job
	}

	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey == "" {
		return CreateSyncJobResult{Job: newJob(), Created: true}, nil
	}

	tuple := strings.Join([]string{in.Scope.Type, in.Scope.ID, in.ProviderID, strings.ToLower(string(in.Mode)), idempotencyKey}, "|")
	if existingID, ok := s.idemRef[tuple]; ok {
		return CreateSyncJobResult{Job: s.jobs[existingID], Created: false}, nil
	}
	job := newJob()
	s.idemRef[tuple] = job.ID
	return CreateSyncJobResult{Job: job, Created: true}, nil
}

func (s *memoryServiceSyncJobStore) GetSyncJob(_ context.Context, id string) (SyncJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[strings.TrimSpace(id)]
	if !ok {
		return SyncJob{}, fmt.Errorf("%w: id %q", ErrSyncJobNotFound, id)
	}
	return job, nil
}

var _ SyncJobStore = (*memoryServiceSyncJobStore)(nil)
