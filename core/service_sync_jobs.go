package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *Service) CreateSyncJob(ctx context.Context, req CreateSyncJobRequest) (result CreateSyncJobResult, err error) {
	startedAt := time.Now().UTC()
	fields := map[string]any{
		"provider_id": req.ProviderID,
		"scope_type":  req.ScopeType,
		"scope_id":    req.ScopeID,
		"mode":        req.Mode,
	}
	defer func() {
		if result.Job.ID != "" {
			fields["sync_job_id"] = result.Job.ID
		}
		fields["created"] = result.Created
		s.observeOperation(ctx, startedAt, "create_sync_job", err, fields)
	}()

	if s == nil || s.syncJobStore == nil {
		err = s.mapError(fmt.Errorf("core: sync job store is required"))
		return CreateSyncJobResult{}, err
	}
	normalized, scope, parseErr := normalizeCreateSyncJobRequest(req)
	if parseErr != nil {
		err = s.mapError(parseErr)
		return CreateSyncJobResult{}, err
	}

	connection, resolveErr := s.resolveSyncJobConnection(ctx, normalized.ProviderID, scope, normalized.ConnectionID)
	if resolveErr != nil {
		err = s.mapError(resolveErr)
		return CreateSyncJobResult{}, err
	}

	storeResult, createErr := s.syncJobStore.CreateSyncJob(ctx, CreateSyncJobStoreInput{
		ProviderID:     normalized.ProviderID,
		Scope:          scope,
		ConnectionID:   connection.ID,
		Mode:           normalized.Mode,
		IdempotencyKey: normalized.IdempotencyKey,
		RequestedBy:    normalized.RequestedBy,
		Metadata:       copyAnyMap(normalized.Metadata),
	})
	if createErr != nil {
		err = s.mapError(createErr)
		return CreateSyncJobResult{}, err
	}
	return storeResult, nil
}

func (s *Service) GetSyncJob(ctx context.Context, req GetSyncJobRequest) (SyncJob, error) {
	if s == nil || s.syncJobStore == nil {
		return SyncJob{}, s.mapError(fmt.Errorf("core: sync job store is required"))
	}
	normalized, scope, hasScopeGuard, parseErr := normalizeGetSyncJobRequest(req)
	if parseErr != nil {
		return SyncJob{}, s.mapError(parseErr)
	}

	job, err := s.syncJobStore.GetSyncJob(ctx, normalized.SyncJobID)
	if err != nil {
		return SyncJob{}, s.mapError(err)
	}

	if normalized.ProviderID != "" &&
		!strings.EqualFold(strings.TrimSpace(job.ProviderID), normalized.ProviderID) {
		return SyncJob{}, s.mapError(fmt.Errorf("%w: id %q", ErrSyncJobNotFound, normalized.SyncJobID))
	}
	if normalized.ConnectionID != "" &&
		!strings.EqualFold(strings.TrimSpace(job.ConnectionID), normalized.ConnectionID) {
		return SyncJob{}, s.mapError(fmt.Errorf("%w: id %q", ErrSyncJobNotFound, normalized.SyncJobID))
	}
	if hasScopeGuard {
		if s.connectionStore == nil {
			return SyncJob{}, s.mapError(fmt.Errorf("core: connection store is required for sync job scope guard"))
		}
		connection, loadErr := s.connectionStore.Get(ctx, strings.TrimSpace(job.ConnectionID))
		if loadErr != nil {
			return SyncJob{}, s.mapError(fmt.Errorf("%w: id %q", ErrSyncJobNotFound, normalized.SyncJobID))
		}
		if !strings.EqualFold(strings.TrimSpace(connection.ScopeType), scope.Type) ||
			strings.TrimSpace(connection.ScopeID) != scope.ID {
			return SyncJob{}, s.mapError(fmt.Errorf("%w: id %q", ErrSyncJobNotFound, normalized.SyncJobID))
		}
	}
	return job, nil
}

func normalizeCreateSyncJobRequest(req CreateSyncJobRequest) (CreateSyncJobRequest, ScopeRef, error) {
	normalized := CreateSyncJobRequest{
		ProviderID:     strings.TrimSpace(req.ProviderID),
		ScopeType:      strings.TrimSpace(strings.ToLower(req.ScopeType)),
		ScopeID:        strings.TrimSpace(req.ScopeID),
		ConnectionID:   strings.TrimSpace(req.ConnectionID),
		Mode:           SyncJobMode(strings.TrimSpace(strings.ToLower(string(req.Mode)))),
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		RequestedBy:    strings.TrimSpace(req.RequestedBy),
		Metadata:       copyAnyMap(req.Metadata),
	}
	if normalized.ProviderID == "" {
		return CreateSyncJobRequest{}, ScopeRef{}, fmt.Errorf("core: provider id is required")
	}
	scope := ScopeRef{
		Type: normalized.ScopeType,
		ID:   normalized.ScopeID,
	}
	if err := scope.Validate(); err != nil {
		return CreateSyncJobRequest{}, ScopeRef{}, fmt.Errorf("%w: %v", ErrInvalidSyncJobScope, err)
	}
	mode, err := parseSyncJobCreateMode(normalized.Mode)
	if err != nil {
		return CreateSyncJobRequest{}, ScopeRef{}, err
	}
	normalized.Mode = mode
	return normalized, scope, nil
}

func normalizeGetSyncJobRequest(req GetSyncJobRequest) (GetSyncJobRequest, ScopeRef, bool, error) {
	normalized := GetSyncJobRequest{
		SyncJobID:    strings.TrimSpace(req.SyncJobID),
		ProviderID:   strings.TrimSpace(req.ProviderID),
		ScopeType:    strings.TrimSpace(strings.ToLower(req.ScopeType)),
		ScopeID:      strings.TrimSpace(req.ScopeID),
		ConnectionID: strings.TrimSpace(req.ConnectionID),
	}
	if normalized.SyncJobID == "" {
		return GetSyncJobRequest{}, ScopeRef{}, false, fmt.Errorf("core: sync job id is required")
	}
	hasScopeType := normalized.ScopeType != ""
	hasScopeID := normalized.ScopeID != ""
	if hasScopeType != hasScopeID {
		return GetSyncJobRequest{}, ScopeRef{}, false, fmt.Errorf("%w: scope type and scope id must both be provided", ErrInvalidSyncJobScope)
	}
	if !hasScopeType {
		return normalized, ScopeRef{}, false, nil
	}
	scope := ScopeRef{
		Type: normalized.ScopeType,
		ID:   normalized.ScopeID,
	}
	if err := scope.Validate(); err != nil {
		return GetSyncJobRequest{}, ScopeRef{}, false, fmt.Errorf("%w: %v", ErrInvalidSyncJobScope, err)
	}
	return normalized, scope, true, nil
}

func parseSyncJobCreateMode(mode SyncJobMode) (SyncJobMode, error) {
	normalized := SyncJobMode(strings.TrimSpace(strings.ToLower(string(mode))))
	switch normalized {
	case SyncJobModeFull, SyncJobModeDelta:
		return normalized, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidSyncJobMode, mode)
	}
}

func (s *Service) resolveSyncJobConnection(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	connectionID string,
) (Connection, error) {
	connectionID = strings.TrimSpace(connectionID)
	if connectionID != "" {
		if s == nil || s.connectionStore == nil {
			return Connection{}, fmt.Errorf("core: connection store is required for explicit sync job connection")
		}
		connection, err := s.connectionStore.Get(ctx, connectionID)
		if err != nil {
			return Connection{}, err
		}
		if !strings.EqualFold(strings.TrimSpace(connection.ProviderID), strings.TrimSpace(providerID)) ||
			!strings.EqualFold(strings.TrimSpace(connection.ScopeType), strings.TrimSpace(scope.Type)) ||
			strings.TrimSpace(connection.ScopeID) != strings.TrimSpace(scope.ID) {
			return Connection{}, fmt.Errorf("%w: connection does not match provider/scope", ErrInvalidSyncJobScope)
		}
		return connection, nil
	}

	resolution, err := s.resolveConnection(ctx, providerID, scope)
	if err != nil {
		return Connection{}, err
	}
	switch resolution.Outcome {
	case ConnectionResolutionDirect, ConnectionResolutionInherited:
		return resolution.Connection, nil
	case ConnectionResolutionAmbiguous:
		return Connection{}, fmt.Errorf("%w: ambiguous connection for provider/scope", ErrInvalidSyncJobScope)
	default:
		return Connection{}, fmt.Errorf("%w: no connection found for provider/scope", ErrInvalidSyncJobScope)
	}
}
