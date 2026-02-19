package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type SyncExecutionServiceOption func(*SyncExecutionService)

func WithSyncExecutionClock(now func() time.Time) SyncExecutionServiceOption {
	return func(s *SyncExecutionService) {
		if s == nil || now == nil {
			return
		}
		s.now = now
	}
}

func WithSyncExecutionEventBus(bus LifecycleEventBus) SyncExecutionServiceOption {
	return func(s *SyncExecutionService) {
		if s == nil {
			return
		}
		s.eventBus = bus
	}
}

type SyncExecutionService struct {
	checkpointStore SyncCheckpointStore
	changeLogStore  SyncChangeLogStore
	eventBus        LifecycleEventBus
	now             func() time.Time
}

func NewSyncExecutionService(
	checkpointStore SyncCheckpointStore,
	changeLogStore SyncChangeLogStore,
	opts ...SyncExecutionServiceOption,
) (*SyncExecutionService, error) {
	if checkpointStore == nil {
		return nil, fmt.Errorf("core: sync checkpoint store is required")
	}
	if changeLogStore == nil {
		return nil, fmt.Errorf("core: sync change log store is required")
	}

	svc := &SyncExecutionService{
		checkpointStore: checkpointStore,
		changeLogStore:  changeLogStore,
		now:             func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc, nil
}

func (s *SyncExecutionService) RunSyncImport(
	ctx context.Context,
	req RunSyncImportRequest,
) (SyncRunResult, error) {
	return s.run(ctx, req.Plan, req.Changes, req.Metadata, SyncDirectionImport)
}

func (s *SyncExecutionService) RunSyncExport(
	ctx context.Context,
	req RunSyncExportRequest,
) (SyncRunResult, error) {
	return s.run(ctx, req.Plan, req.Changes, req.Metadata, SyncDirectionExport)
}

func (s *SyncExecutionService) run(
	ctx context.Context,
	plan SyncRunPlan,
	changes []SyncChange,
	runMetadata map[string]any,
	direction SyncDirection,
) (SyncRunResult, error) {
	if s == nil || s.checkpointStore == nil || s.changeLogStore == nil {
		return SyncRunResult{}, fmt.Errorf("core: sync execution dependencies are required")
	}

	plan.BindingID = strings.TrimSpace(plan.BindingID)
	if plan.BindingID == "" {
		return SyncRunResult{}, fmt.Errorf("core: sync run plan binding id is required")
	}
	if !plan.Mode.IsValid() {
		return SyncRunResult{}, fmt.Errorf("core: invalid sync run mode %q", plan.Mode)
	}

	checkpoint := plan.Checkpoint
	if checkpoint.SyncBindingID == "" {
		checkpoint.SyncBindingID = plan.BindingID
	}
	if checkpoint.SyncBindingID != plan.BindingID {
		return SyncRunResult{}, fmt.Errorf("core: checkpoint binding id does not match plan binding id")
	}
	if checkpoint.Direction == "" {
		checkpoint.Direction = direction
	}
	if checkpoint.Direction != direction {
		return SyncRunResult{}, fmt.Errorf("core: checkpoint direction %q does not match run direction %q", checkpoint.Direction, direction)
	}

	sequence := checkpoint.Sequence
	result := SyncRunResult{
		RunID:    strings.TrimSpace(plan.ID),
		Status:   SyncRunStatusSucceeded,
		Metadata: copyMetadata(runMetadata),
	}
	if result.RunID == "" {
		result.RunID = "run_" + BuildSyncIdempotencyKey(plan.BindingID, direction, checkpoint.Cursor, checkpoint.SourceVersion)[:16]
	}
	if publishErr := s.publishSyncRunEvent(ctx, result.RunID, checkpoint, "services.sync.run.started", map[string]any{
		"status":    string(SyncRunStatusRunning),
		"mode":      string(plan.Mode),
		"direction": string(direction),
	}); publishErr != nil {
		return SyncRunResult{}, publishErr
	}

	for _, change := range changes {
		change = normalizeSyncChange(change)
		if change.ExternalID == "" {
			result.Status = SyncRunStatusFailed
			result.FailedCount++
			result.NextCheckpoint = &checkpoint
			_ = s.publishSyncRunEvent(ctx, result.RunID, checkpoint, "services.sync.run.failed", map[string]any{
				"status": string(result.Status),
				"error":  "core: sync change external id is required",
			})
			return result, fmt.Errorf("core: sync change external id is required")
		}

		sequence++
		checkpoint.Sequence = sequence
		checkpoint.SyncBindingID = plan.BindingID
		checkpoint.Direction = direction
		checkpoint.SourceVersion = change.SourceVersion
		checkpoint.UpdatedAt = s.now()

		if plan.Mode == SyncRunModeDryRun {
			result.ProcessedCount++
			continue
		}

		idempotencyKey := BuildSyncIdempotencyKey(
			plan.BindingID,
			direction,
			change.ExternalID,
			change.SourceVersion,
		)
		entry := SyncChangeLogEntry{
			ProviderID:     checkpoint.ProviderID,
			Scope:          checkpoint.Scope,
			ConnectionID:   checkpoint.ConnectionID,
			SyncBindingID:  plan.BindingID,
			Direction:      direction,
			SourceObject:   change.SourceObject,
			ExternalID:     change.ExternalID,
			SourceVersion:  change.SourceVersion,
			IdempotencyKey: idempotencyKey,
			Payload:        RedactSensitiveMap(change.Payload),
			Metadata:       RedactSensitiveMap(mergeMetadata(change.Metadata, runMetadata)),
			OccurredAt:     s.now(),
		}

		applied, appendErr := s.changeLogStore.Append(ctx, entry)
		if appendErr != nil {
			result.Status = SyncRunStatusFailed
			result.FailedCount++
			result.NextCheckpoint = &checkpoint
			_ = s.publishSyncRunEvent(ctx, result.RunID, checkpoint, "services.sync.run.failed", map[string]any{
				"status": string(result.Status),
				"error":  appendErr.Error(),
			})
			return result, appendErr
		}
		if applied {
			result.ProcessedCount++
		} else {
			result.SkippedCount++
		}

		savedCheckpoint, saveErr := s.checkpointStore.Save(ctx, checkpoint)
		if saveErr != nil {
			result.Status = SyncRunStatusFailed
			result.FailedCount++
			result.NextCheckpoint = &checkpoint
			_ = s.publishSyncRunEvent(ctx, result.RunID, checkpoint, "services.sync.run.failed", map[string]any{
				"status": string(result.Status),
				"error":  saveErr.Error(),
			})
			return result, saveErr
		}
		checkpoint = savedCheckpoint
		if publishErr := s.publishSyncRunEvent(
			ctx,
			result.RunID,
			checkpoint,
			"services.sync.run.checkpoint",
			map[string]any{
				"status":         string(SyncRunStatusRunning),
				"sequence":       checkpoint.Sequence,
				"source_version": checkpoint.SourceVersion,
			},
		); publishErr != nil {
			return SyncRunResult{}, publishErr
		}
	}

	result.NextCheckpoint = &checkpoint
	if publishErr := s.publishSyncRunEvent(
		ctx,
		result.RunID,
		checkpoint,
		"services.sync.run.succeeded",
		map[string]any{
			"status":    string(SyncRunStatusSucceeded),
			"processed": result.ProcessedCount,
			"skipped":   result.SkippedCount,
			"failed":    result.FailedCount,
			"sequence":  checkpoint.Sequence,
		},
	); publishErr != nil {
		return SyncRunResult{}, publishErr
	}
	return result, nil
}

func BuildSyncIdempotencyKey(
	syncBindingID string,
	direction SyncDirection,
	externalID string,
	sourceVersion string,
) string {
	syncBindingID = strings.TrimSpace(strings.ToLower(syncBindingID))
	externalID = strings.TrimSpace(strings.ToLower(externalID))
	sourceVersion = strings.TrimSpace(strings.ToLower(sourceVersion))
	if sourceVersion == "" {
		sourceVersion = "_"
	}
	payload := strings.Join(
		[]string{syncBindingID, string(direction), externalID, sourceVersion},
		"|",
	)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

func normalizeSyncChange(change SyncChange) SyncChange {
	change.SourceObject = strings.TrimSpace(change.SourceObject)
	change.ExternalID = strings.TrimSpace(change.ExternalID)
	change.SourceVersion = strings.TrimSpace(change.SourceVersion)
	if change.Payload == nil {
		change.Payload = make(map[string]any)
	}
	return change
}

func mergeMetadata(left map[string]any, right map[string]any) map[string]any {
	size := len(left) + len(right)
	if size == 0 {
		return make(map[string]any)
	}
	merged := make(map[string]any, size)
	for key, value := range right {
		merged[key] = value
	}
	for key, value := range left {
		merged[key] = value
	}
	return merged
}

func (s *SyncExecutionService) publishSyncRunEvent(
	ctx context.Context,
	runID string,
	checkpoint SyncCheckpoint,
	eventName string,
	payload map[string]any,
) error {
	if s == nil || s.eventBus == nil {
		return nil
	}
	occurredAt := s.now()
	event := LifecycleEvent{
		ID:           buildSyncConflictAuditEventID(runID+"|"+eventName+"|"+fmt.Sprintf("%d", checkpoint.Sequence), eventName, occurredAt),
		Name:         eventName,
		ProviderID:   checkpoint.ProviderID,
		ScopeType:    checkpoint.Scope.Type,
		ScopeID:      checkpoint.Scope.ID,
		ConnectionID: checkpoint.ConnectionID,
		Source:       "services.sync.runs",
		OccurredAt:   occurredAt,
		Payload: mergeMetadata(payload, map[string]any{
			"run_id":          runID,
			"sync_binding_id": checkpoint.SyncBindingID,
			"direction":       string(checkpoint.Direction),
			"sequence":        checkpoint.Sequence,
		}),
		Metadata: RedactSensitiveMap(checkpoint.Metadata),
	}
	return s.eventBus.Publish(ctx, event)
}
