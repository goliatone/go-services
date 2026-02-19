package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SyncPlannerServiceOption func(*SyncPlannerService)

func WithSyncPlannerClock(now func() time.Time) SyncPlannerServiceOption {
	return func(s *SyncPlannerService) {
		if s == nil || now == nil {
			return
		}
		s.now = now
	}
}

type SyncPlannerService struct {
	checkpointStore SyncCheckpointStore
	now             func() time.Time
}

func NewSyncPlannerService(
	checkpointStore SyncCheckpointStore,
	opts ...SyncPlannerServiceOption,
) (*SyncPlannerService, error) {
	if checkpointStore == nil {
		return nil, fmt.Errorf("core: sync checkpoint store is required")
	}
	svc := &SyncPlannerService{
		checkpointStore: checkpointStore,
		now:             func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc, nil
}

func (s *SyncPlannerService) PlanSyncRun(
	ctx context.Context,
	req PlanSyncRunRequest,
) (SyncRunPlan, error) {
	if s == nil || s.checkpointStore == nil {
		return SyncRunPlan{}, fmt.Errorf("core: sync planner checkpoint store is required")
	}

	binding := normalizeSyncBinding(req.Binding)
	if err := binding.Validate(); err != nil {
		return SyncRunPlan{}, err
	}
	if !req.Mode.IsValid() {
		return SyncRunPlan{}, fmt.Errorf("core: invalid sync run mode %q", req.Mode)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	direction := binding.Direction
	checkpoint, found, err := s.resolvePlanCheckpoint(ctx, req, binding, direction)
	if err != nil {
		return SyncRunPlan{}, err
	}
	if !found {
		checkpoint = SyncCheckpoint{
			ProviderID:    binding.ProviderID,
			Scope:         binding.Scope,
			ConnectionID:  binding.ConnectionID,
			SyncBindingID: binding.ID,
			Direction:     direction,
			Metadata:      make(map[string]any),
		}
	}

	metadata := copyMetadata(req.Metadata)
	metadata["planner.limit"] = limit
	metadata["planner.from_checkpoint_id"] = strings.TrimSpace(req.FromCheckpointID)

	plan := SyncRunPlan{
		BindingID:        binding.ID,
		Mode:             req.Mode,
		Checkpoint:       checkpoint,
		EstimatedChanges: limit,
		IdempotencySeed: BuildSyncIdempotencyKey(
			binding.ID,
			direction,
			checkpoint.Cursor,
			checkpoint.SourceVersion,
		),
		Metadata:    metadata,
		GeneratedAt: s.now(),
	}

	planHash, hashErr := buildSyncRunPlanHash(plan)
	if hashErr != nil {
		return SyncRunPlan{}, hashErr
	}
	plan.DeterministicHash = planHash
	plan.ID = "plan_" + planHash[:24]
	return plan, nil
}

func (s *SyncPlannerService) resolvePlanCheckpoint(
	ctx context.Context,
	req PlanSyncRunRequest,
	binding SyncBinding,
	direction SyncDirection,
) (SyncCheckpoint, bool, error) {
	fromCheckpointID := strings.TrimSpace(req.FromCheckpointID)
	if fromCheckpointID != "" {
		checkpoint, found, err := s.checkpointStore.GetByID(
			ctx,
			binding.ProviderID,
			binding.Scope,
			fromCheckpointID,
		)
		if err != nil {
			return SyncCheckpoint{}, false, err
		}
		if !found {
			return SyncCheckpoint{}, false, fmt.Errorf("core: checkpoint %q not found", fromCheckpointID)
		}
		if !checkpointMatchesBinding(checkpoint, binding) {
			return SyncCheckpoint{}, false, fmt.Errorf("core: checkpoint scope/provider mismatch")
		}
		if checkpoint.SyncBindingID != binding.ID {
			return SyncCheckpoint{}, false, fmt.Errorf("core: checkpoint binding id does not match plan binding id")
		}
		if checkpoint.Direction != direction {
			return SyncCheckpoint{}, false, fmt.Errorf("core: checkpoint direction does not match plan direction")
		}
		return checkpoint, true, nil
	}

	checkpoint, found, err := s.checkpointStore.GetLatest(
		ctx,
		binding.ProviderID,
		binding.Scope,
		binding.ID,
		direction,
	)
	if err != nil {
		return SyncCheckpoint{}, false, err
	}
	if found && !checkpointMatchesBinding(checkpoint, binding) {
		return SyncCheckpoint{}, false, fmt.Errorf("core: checkpoint scope/provider mismatch")
	}
	return checkpoint, found, nil
}

func buildSyncRunPlanHash(plan SyncRunPlan) (string, error) {
	payload, err := json.Marshal(struct {
		BindingID        string         `json:"binding_id"`
		Mode             SyncRunMode    `json:"mode"`
		Checkpoint       SyncCheckpoint `json:"checkpoint"`
		EstimatedChanges int            `json:"estimated_changes"`
		IdempotencySeed  string         `json:"idempotency_seed"`
		Metadata         map[string]any `json:"metadata"`
	}{
		BindingID:        plan.BindingID,
		Mode:             plan.Mode,
		Checkpoint:       plan.Checkpoint,
		EstimatedChanges: plan.EstimatedChanges,
		IdempotencySeed:  plan.IdempotencySeed,
		Metadata:         plan.Metadata,
	})
	if err != nil {
		return "", fmt.Errorf("core: marshal sync run plan payload: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func normalizeSyncBinding(binding SyncBinding) SyncBinding {
	binding.ID = strings.TrimSpace(binding.ID)
	binding.ProviderID = strings.TrimSpace(binding.ProviderID)
	binding.Scope = ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(binding.Scope.Type)),
		ID:   strings.TrimSpace(binding.Scope.ID),
	}
	binding.ConnectionID = strings.TrimSpace(binding.ConnectionID)
	binding.MappingSpecID = strings.TrimSpace(binding.MappingSpecID)
	binding.SourceObject = strings.TrimSpace(binding.SourceObject)
	binding.TargetModel = strings.TrimSpace(binding.TargetModel)
	binding.Status = SyncBindingStatus(strings.TrimSpace(strings.ToLower(string(binding.Status))))
	binding.Direction = SyncDirection(strings.TrimSpace(strings.ToLower(string(binding.Direction))))
	return binding
}

func checkpointMatchesBinding(checkpoint SyncCheckpoint, binding SyncBinding) bool {
	return sameProviderScope(checkpoint.ProviderID, checkpoint.Scope, binding.ProviderID, binding.Scope) &&
		strings.TrimSpace(checkpoint.ConnectionID) == strings.TrimSpace(binding.ConnectionID)
}
