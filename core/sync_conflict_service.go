package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type SyncConflictServiceOption func(*SyncConflictLedgerService)

func WithSyncConflictEventBus(bus LifecycleEventBus) SyncConflictServiceOption {
	return func(service *SyncConflictLedgerService) {
		if service == nil {
			return
		}
		service.eventBus = bus
	}
}

func WithSyncConflictPolicyHook(hook SyncConflictPolicyHook) SyncConflictServiceOption {
	return func(service *SyncConflictLedgerService) {
		if service == nil {
			return
		}
		service.policyHook = hook
	}
}

func WithSyncConflictClock(now func() time.Time) SyncConflictServiceOption {
	return func(service *SyncConflictLedgerService) {
		if service == nil || now == nil {
			return
		}
		service.now = now
	}
}

type SyncConflictLedgerService struct {
	store      SyncConflictStore
	eventBus   LifecycleEventBus
	policyHook SyncConflictPolicyHook
	now        func() time.Time
}

func NewSyncConflictLedgerService(
	store SyncConflictStore,
	opts ...SyncConflictServiceOption,
) (*SyncConflictLedgerService, error) {
	if store == nil {
		return nil, fmt.Errorf("core: sync conflict store is required")
	}
	service := &SyncConflictLedgerService{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service, nil
}

func (s *SyncConflictLedgerService) RecordSyncConflict(
	ctx context.Context,
	req RecordSyncConflictRequest,
) (RecordSyncConflictResult, error) {
	if s == nil || s.store == nil {
		return RecordSyncConflictResult{}, fmt.Errorf("core: sync conflict ledger store is required")
	}

	conflict := normalizeSyncConflict(req.Conflict)
	conflict.SourcePayload = RedactSensitiveMap(conflict.SourcePayload)
	conflict.TargetPayload = RedactSensitiveMap(conflict.TargetPayload)
	conflict.Metadata = RedactSensitiveMap(mergeMetadata(conflict.Metadata, req.Metadata))
	if conflict.Status == "" {
		conflict.Status = SyncConflictStatusPending
	}
	conflict.Status = SyncConflictStatusPending

	if s.policyHook != nil {
		policyConflict, hookErr := s.policyHook.ApplyRecordPolicy(ctx, conflict)
		if hookErr != nil {
			return RecordSyncConflictResult{}, hookErr
		}
		conflict = normalizeSyncConflict(policyConflict)
		conflict.Status = SyncConflictStatusPending
	}

	if err := conflict.Validate(); err != nil {
		return RecordSyncConflictResult{}, err
	}

	saved, err := s.store.Append(ctx, conflict)
	if err != nil {
		return RecordSyncConflictResult{}, err
	}
	if publishErr := s.publishAuditEvent(ctx, saved, "services.sync.conflict.recorded", map[string]any{
		"status": string(SyncConflictStatusPending),
	}); publishErr != nil {
		return RecordSyncConflictResult{}, publishErr
	}
	return RecordSyncConflictResult{Conflict: saved}, nil
}

func (s *SyncConflictLedgerService) ResolveSyncConflict(
	ctx context.Context,
	req ResolveSyncConflictRequest,
) (ResolveSyncConflictResult, error) {
	if s == nil || s.store == nil {
		return ResolveSyncConflictResult{}, fmt.Errorf("core: sync conflict ledger store is required")
	}

	req.ConflictID = strings.TrimSpace(req.ConflictID)
	if req.ConflictID == "" {
		return ResolveSyncConflictResult{}, fmt.Errorf("core: conflict id is required")
	}
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	req.Scope = ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(req.Scope.Type)),
		ID:   strings.TrimSpace(req.Scope.ID),
	}
	if req.ProviderID == "" {
		return ResolveSyncConflictResult{}, fmt.Errorf("core: provider id is required")
	}
	if err := req.Scope.Validate(); err != nil {
		return ResolveSyncConflictResult{}, err
	}
	req.Resolution = normalizeSyncConflictResolution(req.Resolution)
	req.Resolution.Patch = RedactSensitiveMap(req.Resolution.Patch)
	if !req.Resolution.Action.IsValid() {
		return ResolveSyncConflictResult{}, fmt.Errorf("core: invalid sync conflict resolution action %q", req.Resolution.Action)
	}

	current, getErr := s.store.Get(ctx, req.ProviderID, req.Scope, req.ConflictID)
	if getErr != nil {
		return ResolveSyncConflictResult{}, getErr
	}
	if !sameProviderScope(current.ProviderID, current.Scope, req.ProviderID, req.Scope) {
		return ResolveSyncConflictResult{}, fmt.Errorf("core: conflict scope mismatch")
	}
	if current.Status == SyncConflictStatusResolved || current.Status == SyncConflictStatusIgnored {
		return ResolveSyncConflictResult{Conflict: current}, nil
	}

	if s.policyHook != nil {
		resolution, hookErr := s.policyHook.ApplyResolutionPolicy(ctx, current, req.Resolution)
		if hookErr != nil {
			return ResolveSyncConflictResult{}, hookErr
		}
		req.Resolution = normalizeSyncConflictResolution(resolution)
		if !req.Resolution.Action.IsValid() {
			return ResolveSyncConflictResult{}, fmt.Errorf("core: invalid sync conflict resolution action %q", req.Resolution.Action)
		}
	}

	resolvedAt := s.now()
	resolved, resolveErr := s.store.Resolve(
		ctx,
		req.ProviderID,
		req.Scope,
		req.ConflictID,
		req.Resolution,
		resolvedAt,
	)
	if resolveErr != nil {
		return ResolveSyncConflictResult{}, resolveErr
	}

	eventName := "services.sync.conflict.resolved"
	switch req.Resolution.Action {
	case SyncConflictResolutionIgnore:
		eventName = "services.sync.conflict.ignored"
	case SyncConflictResolutionRetry:
		eventName = "services.sync.conflict.retry_requested"
	}
	if publishErr := s.publishAuditEvent(ctx, resolved, eventName, map[string]any{
		"action":      string(req.Resolution.Action),
		"reason":      req.Resolution.Reason,
		"resolved_by": req.Resolution.ResolvedBy,
	}); publishErr != nil {
		return ResolveSyncConflictResult{}, publishErr
	}

	return ResolveSyncConflictResult{
		Conflict: resolved,
	}, nil
}

func (s *SyncConflictLedgerService) publishAuditEvent(
	ctx context.Context,
	conflict SyncConflict,
	eventName string,
	payload map[string]any,
) error {
	if s == nil || s.eventBus == nil {
		return nil
	}

	occurredAt := s.now()
	event := LifecycleEvent{
		ID:           buildSyncConflictAuditEventID(conflict.ID, eventName, occurredAt),
		Name:         eventName,
		ProviderID:   conflict.ProviderID,
		ScopeType:    conflict.Scope.Type,
		ScopeID:      conflict.Scope.ID,
		ConnectionID: conflict.ConnectionID,
		Source:       "services.sync.conflicts",
		OccurredAt:   occurredAt,
		Payload: mergeMetadata(payload, map[string]any{
			"conflict_id":      conflict.ID,
			"sync_binding_id":  conflict.SyncBindingID,
			"status":           string(conflict.Status),
			"source_object":    conflict.SourceObject,
			"external_id":      conflict.ExternalID,
			"source_version":   conflict.SourceVersion,
			"idempotency_key":  conflict.IdempotencyKey,
			"resolution_state": string(conflict.Status),
		}),
		Metadata: copyMetadata(conflict.Metadata),
	}
	return s.eventBus.Publish(ctx, event)
}

func buildSyncConflictAuditEventID(
	conflictID string,
	eventName string,
	occurredAt time.Time,
) string {
	payload := strings.Join(
		[]string{
			strings.TrimSpace(conflictID),
			strings.TrimSpace(eventName),
			occurredAt.UTC().Format(time.RFC3339Nano),
		},
		"|",
	)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

func normalizeSyncConflict(conflict SyncConflict) SyncConflict {
	conflict.ID = strings.TrimSpace(conflict.ID)
	conflict.ProviderID = strings.TrimSpace(conflict.ProviderID)
	conflict.Scope = ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(conflict.Scope.Type)),
		ID:   strings.TrimSpace(conflict.Scope.ID),
	}
	conflict.ConnectionID = strings.TrimSpace(conflict.ConnectionID)
	conflict.SyncBindingID = strings.TrimSpace(conflict.SyncBindingID)
	conflict.CheckpointID = strings.TrimSpace(conflict.CheckpointID)
	conflict.SourceObject = strings.TrimSpace(conflict.SourceObject)
	conflict.ExternalID = strings.TrimSpace(conflict.ExternalID)
	conflict.SourceVersion = strings.TrimSpace(conflict.SourceVersion)
	conflict.IdempotencyKey = strings.TrimSpace(conflict.IdempotencyKey)
	conflict.Policy = strings.TrimSpace(conflict.Policy)
	conflict.Reason = strings.TrimSpace(conflict.Reason)
	conflict.ResolvedBy = strings.TrimSpace(conflict.ResolvedBy)
	return conflict
}

func normalizeSyncConflictResolution(resolution SyncConflictResolution) SyncConflictResolution {
	resolution.Action = SyncConflictResolutionAction(strings.TrimSpace(strings.ToLower(string(resolution.Action))))
	resolution.Reason = strings.TrimSpace(resolution.Reason)
	resolution.ResolvedBy = strings.TrimSpace(resolution.ResolvedBy)
	return resolution
}
