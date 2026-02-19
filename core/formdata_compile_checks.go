package core

import (
	"context"
	"time"
)

type compileCheckMappingSpecStore struct{}

func (compileCheckMappingSpecStore) CreateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error) {
	return MappingSpec{}, nil
}

func (compileCheckMappingSpecStore) UpdateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error) {
	return MappingSpec{}, nil
}

func (compileCheckMappingSpecStore) GetVersion(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
	version int,
) (MappingSpec, bool, error) {
	return MappingSpec{}, false, nil
}

func (compileCheckMappingSpecStore) GetLatest(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
) (MappingSpec, bool, error) {
	return MappingSpec{}, false, nil
}

func (compileCheckMappingSpecStore) ListByScope(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
) ([]MappingSpec, error) {
	return nil, nil
}

func (compileCheckMappingSpecStore) PublishVersion(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
	version int,
	publishedAt time.Time,
) (MappingSpec, error) {
	return MappingSpec{}, nil
}

func (compileCheckMappingSpecStore) SetStatus(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
	version int,
	status MappingSpecStatus,
	now time.Time,
) (MappingSpec, error) {
	return MappingSpec{}, nil
}

type compileCheckSyncBindingStore struct{}

func (compileCheckSyncBindingStore) Upsert(ctx context.Context, binding SyncBinding) (SyncBinding, error) {
	return SyncBinding{}, nil
}

func (compileCheckSyncBindingStore) Get(ctx context.Context, id string) (SyncBinding, error) {
	return SyncBinding{}, nil
}

func (compileCheckSyncBindingStore) ListByConnection(
	ctx context.Context,
	connectionID string,
) ([]SyncBinding, error) {
	return nil, nil
}

func (compileCheckSyncBindingStore) UpdateStatus(
	ctx context.Context,
	id string,
	status SyncBindingStatus,
	reason string,
) error {
	return nil
}

type compileCheckIdentityBindingStore struct{}

func (compileCheckIdentityBindingStore) Upsert(
	ctx context.Context,
	binding IdentityBinding,
) (IdentityBinding, error) {
	return IdentityBinding{}, nil
}

func (compileCheckIdentityBindingStore) GetByExternalID(
	ctx context.Context,
	syncBindingID string,
	externalID string,
) (IdentityBinding, bool, error) {
	return IdentityBinding{}, false, nil
}

func (compileCheckIdentityBindingStore) ListByInternalID(
	ctx context.Context,
	syncBindingID string,
	internalType string,
	internalID string,
) ([]IdentityBinding, error) {
	return nil, nil
}

type compileCheckSyncConflictStore struct{}

func (compileCheckSyncConflictStore) Append(ctx context.Context, conflict SyncConflict) (SyncConflict, error) {
	return SyncConflict{}, nil
}

func (compileCheckSyncConflictStore) Get(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	id string,
) (SyncConflict, error) {
	return SyncConflict{}, nil
}

func (compileCheckSyncConflictStore) ListByBinding(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	syncBindingID string,
	status SyncConflictStatus,
) ([]SyncConflict, error) {
	return nil, nil
}

func (compileCheckSyncConflictStore) Resolve(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	id string,
	resolution SyncConflictResolution,
	resolvedAt time.Time,
) (SyncConflict, error) {
	return SyncConflict{}, nil
}

type compileCheckSyncCheckpointStore struct{}

func (compileCheckSyncCheckpointStore) Save(
	ctx context.Context,
	checkpoint SyncCheckpoint,
) (SyncCheckpoint, error) {
	return SyncCheckpoint{}, nil
}

func (compileCheckSyncCheckpointStore) GetByID(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	id string,
) (SyncCheckpoint, bool, error) {
	return SyncCheckpoint{}, false, nil
}

func (compileCheckSyncCheckpointStore) GetLatest(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	syncBindingID string,
	direction SyncDirection,
) (SyncCheckpoint, bool, error) {
	return SyncCheckpoint{}, false, nil
}

type compileCheckSyncChangeLogStore struct{}

func (compileCheckSyncChangeLogStore) Append(
	ctx context.Context,
	entry SyncChangeLogEntry,
) (bool, error) {
	return true, nil
}

func (compileCheckSyncChangeLogStore) ListSince(
	ctx context.Context,
	syncBindingID string,
	direction SyncDirection,
	cursor string,
	limit int,
) ([]SyncChangeLogEntry, string, error) {
	return nil, "", nil
}

type compileCheckExternalFormDataService struct{}

func (compileCheckExternalFormDataService) ValidateMappingSpec(
	ctx context.Context,
	req ValidateMappingSpecRequest,
) (ValidateMappingSpecResult, error) {
	return ValidateMappingSpecResult{}, nil
}

func (compileCheckExternalFormDataService) PreviewMappingSpec(
	ctx context.Context,
	req PreviewMappingSpecRequest,
) (PreviewMappingSpecResult, error) {
	return PreviewMappingSpecResult{}, nil
}

func (compileCheckExternalFormDataService) PlanSyncRun(
	ctx context.Context,
	req PlanSyncRunRequest,
) (SyncRunPlan, error) {
	return SyncRunPlan{}, nil
}

func (compileCheckExternalFormDataService) RunSyncImport(
	ctx context.Context,
	req RunSyncImportRequest,
) (SyncRunResult, error) {
	return SyncRunResult{}, nil
}

func (compileCheckExternalFormDataService) RunSyncExport(
	ctx context.Context,
	req RunSyncExportRequest,
) (SyncRunResult, error) {
	return SyncRunResult{}, nil
}

func (compileCheckExternalFormDataService) ResolveSyncConflict(
	ctx context.Context,
	req ResolveSyncConflictRequest,
) (ResolveSyncConflictResult, error) {
	return ResolveSyncConflictResult{}, nil
}

type compileCheckMappingSpecCompiler struct{}

func (compileCheckMappingSpecCompiler) CompileMappingSpec(
	ctx context.Context,
	req ValidateMappingSpecRequest,
) (CompiledMappingSpec, []MappingValidationIssue, error) {
	return CompiledMappingSpec{}, nil, nil
}

type compileCheckSyncConflictPolicyHook struct{}

func (compileCheckSyncConflictPolicyHook) ApplyRecordPolicy(
	ctx context.Context,
	conflict SyncConflict,
) (SyncConflict, error) {
	return conflict, nil
}

func (compileCheckSyncConflictPolicyHook) ApplyResolutionPolicy(
	ctx context.Context,
	conflict SyncConflict,
	resolution SyncConflictResolution,
) (SyncConflictResolution, error) {
	return resolution, nil
}

var (
	_ MappingSpecStore            = (*compileCheckMappingSpecStore)(nil)
	_ SyncBindingStore            = (*compileCheckSyncBindingStore)(nil)
	_ IdentityBindingStore        = (*compileCheckIdentityBindingStore)(nil)
	_ SyncConflictStore           = (*compileCheckSyncConflictStore)(nil)
	_ SyncCheckpointStore         = (*compileCheckSyncCheckpointStore)(nil)
	_ SyncChangeLogStore          = (*compileCheckSyncChangeLogStore)(nil)
	_ MappingSpecValidator        = (*compileCheckExternalFormDataService)(nil)
	_ MappingSpecPreviewer        = (*compileCheckExternalFormDataService)(nil)
	_ SyncPlanner                 = (*compileCheckExternalFormDataService)(nil)
	_ SyncRunner                  = (*compileCheckExternalFormDataService)(nil)
	_ SyncConflictResolver        = (*compileCheckExternalFormDataService)(nil)
	_ ExternalFormDataService     = (*compileCheckExternalFormDataService)(nil)
	_ MappingSpecCompiler         = (*compileCheckMappingSpecCompiler)(nil)
	_ MappingSpecValidator        = (*MappingCompiler)(nil)
	_ MappingSpecCompiler         = (*MappingCompiler)(nil)
	_ MappingSpecPreviewer        = (*MappingPreviewer)(nil)
	_ IdentityReconciler          = (*IdentityBindingReconciler)(nil)
	_ SyncPlanner                 = (*SyncPlannerService)(nil)
	_ SyncRunner                  = (*SyncExecutionService)(nil)
	_ SyncConflictRecorder        = (*SyncConflictLedgerService)(nil)
	_ SyncConflictResolver        = (*SyncConflictLedgerService)(nil)
	_ SyncConflictService         = (*SyncConflictLedgerService)(nil)
	_ SyncConflictPolicyHook      = (*compileCheckSyncConflictPolicyHook)(nil)
	_ MappingSpecLifecycleService = (*MappingSpecLifecycle)(nil)
)
