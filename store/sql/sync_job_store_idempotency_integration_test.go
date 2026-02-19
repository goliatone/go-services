package sqlstore_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/goliatone/go-services/core"
	sqlstore "github.com/goliatone/go-services/store/sql"
)

func TestSyncJobStore_CreateSyncJob_IdempotencySemantics(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	connection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "org", ID: "org_sync_job_1"},
		ExternalAccountID: "acct_sync_job_1",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	store, err := sqlstore.NewSyncJobStore(client.DB())
	if err != nil {
		t.Fatalf("new sync job store: %v", err)
	}

	first, err := store.CreateSyncJob(ctx, core.CreateSyncJobStoreInput{
		ProviderID:     "github",
		Scope:          core.ScopeRef{Type: "org", ID: "org_sync_job_1"},
		ConnectionID:   connection.ID,
		Mode:           core.SyncJobModeFull,
		IdempotencyKey: "idem_sync_job_1",
		RequestedBy:    "wizard",
	})
	if err != nil {
		t.Fatalf("create sync job first: %v", err)
	}
	if !first.Created {
		t.Fatalf("expected first create to have Created=true")
	}

	replay, err := store.CreateSyncJob(ctx, core.CreateSyncJobStoreInput{
		ProviderID:     "github",
		Scope:          core.ScopeRef{Type: "org", ID: "org_sync_job_1"},
		ConnectionID:   connection.ID,
		Mode:           core.SyncJobModeFull,
		IdempotencyKey: "idem_sync_job_1",
	})
	if err != nil {
		t.Fatalf("create sync job replay: %v", err)
	}
	if replay.Created {
		t.Fatalf("expected replay to have Created=false")
	}
	if replay.Job.ID != first.Job.ID {
		t.Fatalf("expected replay id %q, got %q", first.Job.ID, replay.Job.ID)
	}

	otherKey, err := store.CreateSyncJob(ctx, core.CreateSyncJobStoreInput{
		ProviderID:     "github",
		Scope:          core.ScopeRef{Type: "org", ID: "org_sync_job_1"},
		ConnectionID:   connection.ID,
		Mode:           core.SyncJobModeFull,
		IdempotencyKey: "idem_sync_job_2",
	})
	if err != nil {
		t.Fatalf("create sync job with different key: %v", err)
	}
	if !otherKey.Created {
		t.Fatalf("expected different key to create a new job")
	}
	if otherKey.Job.ID == first.Job.ID {
		t.Fatalf("expected different key to yield different sync job id")
	}

	noKeyA, err := store.CreateSyncJob(ctx, core.CreateSyncJobStoreInput{
		ProviderID:   "github",
		Scope:        core.ScopeRef{Type: "org", ID: "org_sync_job_1"},
		ConnectionID: connection.ID,
		Mode:         core.SyncJobModeDelta,
	})
	if err != nil {
		t.Fatalf("create sync job without key first: %v", err)
	}
	noKeyB, err := store.CreateSyncJob(ctx, core.CreateSyncJobStoreInput{
		ProviderID:   "github",
		Scope:        core.ScopeRef{Type: "org", ID: "org_sync_job_1"},
		ConnectionID: connection.ID,
		Mode:         core.SyncJobModeDelta,
	})
	if err != nil {
		t.Fatalf("create sync job without key second: %v", err)
	}
	if noKeyA.Job.ID == noKeyB.Job.ID {
		t.Fatalf("expected empty idempotency key to create a new job each call")
	}
}

func TestSyncJobStore_GetSyncJob_ExistingAndMissing(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	connection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "org", ID: "org_sync_job_get"},
		ExternalAccountID: "acct_sync_job_get",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	store, err := sqlstore.NewSyncJobStore(client.DB())
	if err != nil {
		t.Fatalf("new sync job store: %v", err)
	}

	created, err := store.CreateSyncJob(ctx, core.CreateSyncJobStoreInput{
		ProviderID:     "github",
		Scope:          core.ScopeRef{Type: "org", ID: "org_sync_job_get"},
		ConnectionID:   connection.ID,
		Mode:           core.SyncJobModeFull,
		IdempotencyKey: "idem_sync_job_get",
	})
	if err != nil {
		t.Fatalf("create sync job: %v", err)
	}

	loaded, err := store.GetSyncJob(ctx, created.Job.ID)
	if err != nil {
		t.Fatalf("get sync job: %v", err)
	}
	if loaded.ID != created.Job.ID {
		t.Fatalf("expected loaded id %q, got %q", created.Job.ID, loaded.ID)
	}

	_, err = store.GetSyncJob(ctx, "missing")
	if !errors.Is(err, core.ErrSyncJobNotFound) {
		t.Fatalf("expected sync job not found error, got %v", err)
	}
}

func TestSyncJobStore_CreateSyncJob_ConcurrentReplaySingleWinner(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	connection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "org", ID: "org_sync_job_parallel"},
		ExternalAccountID: "acct_sync_job_parallel",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	store, err := sqlstore.NewSyncJobStore(client.DB())
	if err != nil {
		t.Fatalf("new sync job store: %v", err)
	}

	const workers = 16
	type result struct {
		id      string
		created bool
		err     error
	}
	results := make(chan result, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, callErr := store.CreateSyncJob(ctx, core.CreateSyncJobStoreInput{
				ProviderID:     "github",
				Scope:          core.ScopeRef{Type: "org", ID: "org_sync_job_parallel"},
				ConnectionID:   connection.ID,
				Mode:           core.SyncJobModeFull,
				IdempotencyKey: "idem_parallel_1",
				RequestedBy:    fmt.Sprintf("worker_%d", i),
			})
			results <- result{id: out.Job.ID, created: out.Created, err: callErr}
		}(i)
	}
	wg.Wait()
	close(results)

	uniqueIDs := map[string]struct{}{}
	createdCount := 0
	for item := range results {
		if item.err != nil {
			t.Fatalf("parallel create sync job: %v", item.err)
		}
		uniqueIDs[item.id] = struct{}{}
		if item.created {
			createdCount++
		}
	}
	if len(uniqueIDs) != 1 {
		t.Fatalf("expected exactly one unique sync job id, got %d", len(uniqueIDs))
	}
	if createdCount != 1 {
		t.Fatalf("expected exactly one created=true result, got %d", createdCount)
	}
}
