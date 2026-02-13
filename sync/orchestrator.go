package sync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/google/uuid"
)

type SyncJobStore interface {
	Create(ctx context.Context, job core.SyncJob) (core.SyncJob, error)
	Get(ctx context.Context, id string) (core.SyncJob, error)
	Update(ctx context.Context, job core.SyncJob) (core.SyncJob, error)
}

type Orchestrator struct {
	Jobs    SyncJobStore
	Cursors core.SyncCursorStore
	Now     func() time.Time
}

func NewOrchestrator(jobs SyncJobStore, cursors core.SyncCursorStore) *Orchestrator {
	return &Orchestrator{
		Jobs:    jobs,
		Cursors: cursors,
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (o *Orchestrator) StartBootstrap(ctx context.Context, req core.BootstrapRequest) (core.SyncJob, error) {
	return o.start(ctx, core.SyncJob{
		ConnectionID: strings.TrimSpace(req.ConnectionID),
		ProviderID:   strings.TrimSpace(req.ProviderID),
		Mode:         core.SyncJobModeBootstrap,
		Status:       core.SyncJobStatusQueued,
		Metadata: map[string]any{
			"resource_type": strings.TrimSpace(req.ResourceType),
			"resource_id":   strings.TrimSpace(req.ResourceID),
		},
	}, req.ResourceType, req.ResourceID, req.Metadata)
}

func (o *Orchestrator) StartBackfill(ctx context.Context, req core.BackfillRequest) (core.SyncJob, error) {
	metadata := map[string]any{
		"resource_type": strings.TrimSpace(req.ResourceType),
		"resource_id":   strings.TrimSpace(req.ResourceID),
	}
	if req.From != nil {
		metadata["from"] = req.From.UTC().Format(time.RFC3339Nano)
	}
	if req.To != nil {
		metadata["to"] = req.To.UTC().Format(time.RFC3339Nano)
	}
	return o.start(ctx, core.SyncJob{
		ConnectionID: strings.TrimSpace(req.ConnectionID),
		ProviderID:   strings.TrimSpace(req.ProviderID),
		Mode:         core.SyncJobModeBackfill,
		Status:       core.SyncJobStatusQueued,
		Metadata:     metadata,
	}, req.ResourceType, req.ResourceID, req.Metadata)
}

func (o *Orchestrator) StartIncremental(
	ctx context.Context,
	connectionID string,
	providerID string,
	resourceType string,
	resourceID string,
	metadata map[string]any,
) (core.SyncJob, error) {
	return o.start(ctx, core.SyncJob{
		ConnectionID: strings.TrimSpace(connectionID),
		ProviderID:   strings.TrimSpace(providerID),
		Mode:         core.SyncJobModeIncremental,
		Status:       core.SyncJobStatusQueued,
		Metadata: map[string]any{
			"resource_type": strings.TrimSpace(resourceType),
			"resource_id":   strings.TrimSpace(resourceID),
		},
	}, resourceType, resourceID, metadata)
}

func (o *Orchestrator) Resume(ctx context.Context, jobID string) error {
	if o == nil || o.Jobs == nil {
		return fmt.Errorf("sync: orchestrator requires sync job store")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return fmt.Errorf("sync: job id is required")
	}
	job, err := o.Jobs.Get(ctx, jobID)
	if err != nil {
		return err
	}

	switch job.Status {
	case core.SyncJobStatusFailed:
		job.Status = core.SyncJobStatusQueued
	case core.SyncJobStatusSucceeded:
		return nil
	}
	job.Attempts++
	job.UpdatedAt = o.now()
	_, err = o.Jobs.Update(ctx, job)
	return err
}

func (o *Orchestrator) SaveCheckpoint(
	ctx context.Context,
	jobID string,
	checkpoint string,
	metadata map[string]any,
) (core.SyncJob, error) {
	if o == nil || o.Jobs == nil {
		return core.SyncJob{}, fmt.Errorf("sync: orchestrator requires sync job store")
	}
	job, err := o.Jobs.Get(ctx, strings.TrimSpace(jobID))
	if err != nil {
		return core.SyncJob{}, err
	}
	job.Checkpoint = strings.TrimSpace(checkpoint)
	job.Status = core.SyncJobStatusRunning
	job.UpdatedAt = o.now()
	job.Metadata = mergeAnyMap(job.Metadata, metadata)
	return o.Jobs.Update(ctx, job)
}

func (o *Orchestrator) Complete(
	ctx context.Context,
	jobID string,
	checkpoint string,
	metadata map[string]any,
) (core.SyncJob, error) {
	job, err := o.SaveCheckpoint(ctx, jobID, checkpoint, metadata)
	if err != nil {
		return core.SyncJob{}, err
	}
	job.Status = core.SyncJobStatusSucceeded
	job.UpdatedAt = o.now()
	return o.Jobs.Update(ctx, job)
}

func (o *Orchestrator) Fail(
	ctx context.Context,
	jobID string,
	cause error,
	nextAttemptAt *time.Time,
) (core.SyncJob, error) {
	if o == nil || o.Jobs == nil {
		return core.SyncJob{}, fmt.Errorf("sync: orchestrator requires sync job store")
	}
	job, err := o.Jobs.Get(ctx, strings.TrimSpace(jobID))
	if err != nil {
		return core.SyncJob{}, err
	}
	job.Status = core.SyncJobStatusFailed
	job.Attempts++
	job.UpdatedAt = o.now()
	job.Metadata = mergeAnyMap(job.Metadata, map[string]any{
		"last_error": strings.TrimSpace(fmt.Sprint(cause)),
	})
	if nextAttemptAt != nil {
		value := nextAttemptAt.UTC()
		job.NextAttemptAt = &value
	}
	return o.Jobs.Update(ctx, job)
}

func (o *Orchestrator) start(
	ctx context.Context,
	job core.SyncJob,
	resourceType string,
	resourceID string,
	metadata map[string]any,
) (core.SyncJob, error) {
	if o == nil || o.Jobs == nil {
		return core.SyncJob{}, fmt.Errorf("sync: orchestrator requires sync job store")
	}
	if strings.TrimSpace(job.ConnectionID) == "" || strings.TrimSpace(job.ProviderID) == "" {
		return core.SyncJob{}, fmt.Errorf("sync: connection id and provider id are required")
	}

	now := o.now()
	job.ID = uuid.NewString()
	job.Attempts = 0
	job.CreatedAt = now
	job.UpdatedAt = now
	job.Metadata = mergeAnyMap(job.Metadata, metadata)

	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if o.Cursors != nil && resourceType != "" && resourceID != "" {
		cursor, err := o.Cursors.Get(ctx, job.ConnectionID, resourceType, resourceID)
		if err == nil {
			job.Checkpoint = strings.TrimSpace(cursor.Cursor)
		}
	}

	return o.Jobs.Create(ctx, job)
}

func (o *Orchestrator) now() time.Time {
	if o != nil && o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}

func mergeAnyMap(left map[string]any, right map[string]any) map[string]any {
	if len(left) == 0 && len(right) == 0 {
		return map[string]any{}
	}
	merged := map[string]any{}
	for key, value := range left {
		merged[key] = value
	}
	for key, value := range right {
		merged[key] = value
	}
	return merged
}

var _ core.BulkSyncOrchestrator = (*Orchestrator)(nil)
