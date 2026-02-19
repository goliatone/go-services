package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type MappingSpecStatus string

const (
	MappingSpecStatusDraft     MappingSpecStatus = "draft"
	MappingSpecStatusValidated MappingSpecStatus = "validated"
	MappingSpecStatusPublished MappingSpecStatus = "published"
)

func (s MappingSpecStatus) IsValid() bool {
	switch s {
	case MappingSpecStatusDraft, MappingSpecStatusValidated, MappingSpecStatusPublished:
		return true
	default:
		return false
	}
}

type SyncDirection string

const (
	SyncDirectionImport SyncDirection = "import"
	SyncDirectionExport SyncDirection = "export"
)

func (d SyncDirection) IsValid() bool {
	switch d {
	case SyncDirectionImport, SyncDirectionExport:
		return true
	default:
		return false
	}
}

type SyncRunMode string

const (
	SyncRunModeDryRun SyncRunMode = "dry_run"
	SyncRunModeApply  SyncRunMode = "apply"
)

func (m SyncRunMode) IsValid() bool {
	switch m {
	case SyncRunModeDryRun, SyncRunModeApply:
		return true
	default:
		return false
	}
}

type SyncRunStatus string

const (
	SyncRunStatusPlanned   SyncRunStatus = "planned"
	SyncRunStatusRunning   SyncRunStatus = "running"
	SyncRunStatusSucceeded SyncRunStatus = "succeeded"
	SyncRunStatusFailed    SyncRunStatus = "failed"
)

func (s SyncRunStatus) IsValid() bool {
	switch s {
	case SyncRunStatusPlanned, SyncRunStatusRunning, SyncRunStatusSucceeded, SyncRunStatusFailed:
		return true
	default:
		return false
	}
}

type SyncBindingStatus string

const (
	SyncBindingStatusActive SyncBindingStatus = "active"
	SyncBindingStatusPaused SyncBindingStatus = "paused"
)

func (s SyncBindingStatus) IsValid() bool {
	switch s {
	case SyncBindingStatusActive, SyncBindingStatusPaused:
		return true
	default:
		return false
	}
}

type IdentityBindingMatchKind string

const (
	IdentityBindingMatchExact      IdentityBindingMatchKind = "exact"
	IdentityBindingMatchConfident  IdentityBindingMatchKind = "confident"
	IdentityBindingMatchAmbiguous  IdentityBindingMatchKind = "ambiguous"
	IdentityBindingMatchUnresolved IdentityBindingMatchKind = "unresolved"
)

func (m IdentityBindingMatchKind) IsValid() bool {
	switch m {
	case IdentityBindingMatchExact,
		IdentityBindingMatchConfident,
		IdentityBindingMatchAmbiguous,
		IdentityBindingMatchUnresolved:
		return true
	default:
		return false
	}
}

type SyncConflictStatus string

const (
	SyncConflictStatusPending  SyncConflictStatus = "pending"
	SyncConflictStatusResolved SyncConflictStatus = "resolved"
	SyncConflictStatusIgnored  SyncConflictStatus = "ignored"
)

func (s SyncConflictStatus) IsValid() bool {
	switch s {
	case SyncConflictStatusPending, SyncConflictStatusResolved, SyncConflictStatusIgnored:
		return true
	default:
		return false
	}
}

type SyncConflictResolutionAction string

const (
	SyncConflictResolutionResolve SyncConflictResolutionAction = "resolve"
	SyncConflictResolutionIgnore  SyncConflictResolutionAction = "ignore"
	SyncConflictResolutionRetry   SyncConflictResolutionAction = "retry"
)

func (a SyncConflictResolutionAction) IsValid() bool {
	switch a {
	case SyncConflictResolutionResolve, SyncConflictResolutionIgnore, SyncConflictResolutionRetry:
		return true
	default:
		return false
	}
}

type ExternalField struct {
	Path        string
	Type        string
	Required    bool
	Repeatable  bool
	Format      string
	Constraints map[string]any
	Metadata    map[string]any
}

type ExternalObjectSchema struct {
	Name       string
	PrimaryKey []string
	Fields     []ExternalField
	Metadata   map[string]any
}

type ExternalSchema struct {
	ID         string
	ProviderID string
	Scope      ScopeRef
	Name       string
	Version    string
	Objects    []ExternalObjectSchema
	Metadata   map[string]any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (s ExternalSchema) Validate() error {
	if strings.TrimSpace(s.ProviderID) == "" {
		return fmt.Errorf("core: provider id is required")
	}
	if err := s.Scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("core: schema name is required")
	}
	if len(s.Objects) == 0 {
		return fmt.Errorf("core: schema must define at least one object")
	}
	return nil
}

type MappingRule struct {
	ID          string
	SourcePath  string
	TargetPath  string
	Transform   string
	Required    bool
	Default     any
	Constraints map[string]any
	Metadata    map[string]any
}

func (r MappingRule) Validate() error {
	if strings.TrimSpace(r.SourcePath) == "" {
		return fmt.Errorf("core: rule source path is required")
	}
	if strings.TrimSpace(r.TargetPath) == "" {
		return fmt.Errorf("core: rule target path is required")
	}
	return nil
}

type MappingSpec struct {
	ID           string
	SpecID       string
	ProviderID   string
	Scope        ScopeRef
	Name         string
	Description  string
	SourceObject string
	TargetModel  string
	SchemaRef    string
	Version      int
	Status       MappingSpecStatus
	Rules        []MappingRule
	Metadata     map[string]any
	CreatedAt    time.Time
	UpdatedAt    time.Time
	PublishedAt  *time.Time
}

func (s MappingSpec) Validate() error {
	if strings.TrimSpace(s.SpecID) == "" {
		return fmt.Errorf("core: mapping spec id is required")
	}
	if strings.TrimSpace(s.ProviderID) == "" {
		return fmt.Errorf("core: provider id is required")
	}
	if err := s.Scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("core: mapping spec name is required")
	}
	if strings.TrimSpace(s.SourceObject) == "" {
		return fmt.Errorf("core: source object is required")
	}
	if strings.TrimSpace(s.TargetModel) == "" {
		return fmt.Errorf("core: target model is required")
	}
	if s.Version < 1 {
		return fmt.Errorf("core: mapping spec version must be >= 1")
	}
	if !s.Status.IsValid() {
		return fmt.Errorf("core: invalid mapping spec status %q", s.Status)
	}
	for idx, rule := range s.Rules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("core: invalid mapping rule at index %d: %w", idx, err)
		}
	}
	return nil
}

type IdentityBinding struct {
	ID            string
	ProviderID    string
	Scope         ScopeRef
	ConnectionID  string
	SyncBindingID string
	SourceObject  string
	ExternalID    string
	InternalType  string
	InternalID    string
	MatchKind     IdentityBindingMatchKind
	Confidence    float64
	Metadata      map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (b IdentityBinding) Validate() error {
	if strings.TrimSpace(b.ProviderID) == "" {
		return fmt.Errorf("core: provider id is required")
	}
	if err := b.Scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(b.ConnectionID) == "" {
		return fmt.Errorf("core: connection id is required")
	}
	if strings.TrimSpace(b.SourceObject) == "" {
		return fmt.Errorf("core: source object is required")
	}
	if strings.TrimSpace(b.ExternalID) == "" {
		return fmt.Errorf("core: external id is required")
	}
	if strings.TrimSpace(b.InternalID) == "" {
		return fmt.Errorf("core: internal id is required")
	}
	if !b.MatchKind.IsValid() {
		return fmt.Errorf("core: invalid identity match kind %q", b.MatchKind)
	}
	if b.Confidence < 0 || b.Confidence > 1 {
		return fmt.Errorf("core: identity confidence must be in [0,1]")
	}
	return nil
}

type IdentityCandidate struct {
	InternalType string
	InternalID   string
	Confidence   float64
	Metadata     map[string]any
}

type ReconcileIdentityRequest struct {
	ProviderID    string
	Scope         ScopeRef
	ConnectionID  string
	SyncBindingID string
	SourceObject  string
	ExternalID    string
	Candidates    []IdentityCandidate
	Metadata      map[string]any
}

type ReconcileIdentityResult struct {
	Binding IdentityBinding
	Created bool
}

type SyncBinding struct {
	ID            string
	ProviderID    string
	Scope         ScopeRef
	ConnectionID  string
	MappingSpecID string
	SourceObject  string
	TargetModel   string
	Direction     SyncDirection
	Status        SyncBindingStatus
	Metadata      map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (b SyncBinding) Validate() error {
	if strings.TrimSpace(b.ProviderID) == "" {
		return fmt.Errorf("core: provider id is required")
	}
	if err := b.Scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(b.ConnectionID) == "" {
		return fmt.Errorf("core: connection id is required")
	}
	if strings.TrimSpace(b.MappingSpecID) == "" {
		return fmt.Errorf("core: mapping spec id is required")
	}
	if strings.TrimSpace(b.SourceObject) == "" {
		return fmt.Errorf("core: source object is required")
	}
	if strings.TrimSpace(b.TargetModel) == "" {
		return fmt.Errorf("core: target model is required")
	}
	if !b.Direction.IsValid() {
		return fmt.Errorf("core: invalid sync direction %q", b.Direction)
	}
	if !b.Status.IsValid() {
		return fmt.Errorf("core: invalid sync binding status %q", b.Status)
	}
	return nil
}

type SyncConflict struct {
	ID             string
	ProviderID     string
	Scope          ScopeRef
	ConnectionID   string
	SyncBindingID  string
	CheckpointID   string
	SourceObject   string
	ExternalID     string
	SourceVersion  string
	IdempotencyKey string
	Policy         string
	Reason         string
	Status         SyncConflictStatus
	SourcePayload  map[string]any
	TargetPayload  map[string]any
	Resolution     map[string]any
	ResolvedBy     string
	ResolvedAt     *time.Time
	Metadata       map[string]any
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (c SyncConflict) Validate() error {
	if strings.TrimSpace(c.ProviderID) == "" {
		return fmt.Errorf("core: provider id is required")
	}
	if err := c.Scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.ConnectionID) == "" {
		return fmt.Errorf("core: connection id is required")
	}
	if strings.TrimSpace(c.SyncBindingID) == "" {
		return fmt.Errorf("core: sync binding id is required")
	}
	if strings.TrimSpace(c.SourceObject) == "" {
		return fmt.Errorf("core: source object is required")
	}
	if strings.TrimSpace(c.ExternalID) == "" {
		return fmt.Errorf("core: external id is required")
	}
	if strings.TrimSpace(c.Reason) == "" {
		return fmt.Errorf("core: conflict reason is required")
	}
	if !c.Status.IsValid() {
		return fmt.Errorf("core: invalid sync conflict status %q", c.Status)
	}
	return nil
}

type SyncCheckpoint struct {
	ID              string
	ProviderID      string
	Scope           ScopeRef
	ConnectionID    string
	SyncBindingID   string
	Direction       SyncDirection
	Cursor          string
	Sequence        int64
	SourceVersion   string
	IdempotencySeed string
	Metadata        map[string]any
	LastEventAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (c SyncCheckpoint) Validate() error {
	if strings.TrimSpace(c.ProviderID) == "" {
		return fmt.Errorf("core: provider id is required")
	}
	if err := c.Scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.ConnectionID) == "" {
		return fmt.Errorf("core: connection id is required")
	}
	if strings.TrimSpace(c.SyncBindingID) == "" {
		return fmt.Errorf("core: sync binding id is required")
	}
	if !c.Direction.IsValid() {
		return fmt.Errorf("core: invalid checkpoint direction %q", c.Direction)
	}
	if c.Sequence < 0 {
		return fmt.Errorf("core: checkpoint sequence must be >= 0")
	}
	return nil
}

type MappingValidationIssueSeverity string

const (
	MappingValidationIssueError   MappingValidationIssueSeverity = "error"
	MappingValidationIssueWarning MappingValidationIssueSeverity = "warning"
)

type MappingValidationIssue struct {
	Code       string
	Message    string
	Severity   MappingValidationIssueSeverity
	RuleID     string
	SourcePath string
	TargetPath string
}

type ValidateMappingSpecRequest struct {
	Spec   MappingSpec
	Schema ExternalSchema
}

type CompiledMappingRule struct {
	Rule       MappingRule
	SourceType string
	TargetType string
	Transform  string
}

type CompiledMappingSpec struct {
	SpecID            string
	Version           int
	SourceObject      string
	Rules             []CompiledMappingRule
	DeterministicHash string
}

type ValidateMappingSpecResult struct {
	Valid          bool
	Issues         []MappingValidationIssue
	NormalizedSpec MappingSpec
	Compiled       CompiledMappingSpec
}

type PreviewMappingSpecRequest struct {
	Spec    MappingSpec
	Schema  ExternalSchema
	Samples []map[string]any
}

type PreviewFieldDiff struct {
	RuleID      string
	SourcePath  string
	TargetPath  string
	InputValue  any
	OutputValue any
	Changed     bool
}

type PreviewRecord struct {
	Input  map[string]any
	Output map[string]any
	Diff   []PreviewFieldDiff
	Issues []MappingValidationIssue
}

type PreviewReport struct {
	SampleCount      int
	IssueCount       int
	AppliedRuleCount int
}

type PreviewMappingSpecResult struct {
	Issues            []MappingValidationIssue
	Records           []PreviewRecord
	Report            PreviewReport
	DeterministicHash string
	GeneratedAt       time.Time
}

type PlanSyncRunRequest struct {
	Binding          SyncBinding
	Mode             SyncRunMode
	FromCheckpointID string
	Limit            int
	Metadata         map[string]any
}

type SyncRunPlan struct {
	ID                string
	BindingID         string
	Mode              SyncRunMode
	Checkpoint        SyncCheckpoint
	EstimatedChanges  int
	IdempotencySeed   string
	DeterministicHash string
	Metadata          map[string]any
	GeneratedAt       time.Time
}

type SyncChange struct {
	SourceObject  string
	ExternalID    string
	SourceVersion string
	Payload       map[string]any
	Metadata      map[string]any
}

type RunSyncImportRequest struct {
	Plan     SyncRunPlan
	Changes  []SyncChange
	Metadata map[string]any
}

type RunSyncExportRequest struct {
	Plan     SyncRunPlan
	Changes  []SyncChange
	Metadata map[string]any
}

type SyncRunResult struct {
	RunID          string
	Status         SyncRunStatus
	ProcessedCount int
	SkippedCount   int
	ConflictCount  int
	FailedCount    int
	NextCheckpoint *SyncCheckpoint
	Metadata       map[string]any
}

type SyncConflictResolution struct {
	Action     SyncConflictResolutionAction
	Patch      map[string]any
	Reason     string
	ResolvedBy string
}

type ResolveSyncConflictRequest struct {
	ProviderID string
	Scope      ScopeRef
	ConflictID string
	Resolution SyncConflictResolution
	Metadata   map[string]any
}

type ResolveSyncConflictResult struct {
	Conflict       SyncConflict
	NextCheckpoint *SyncCheckpoint
}

type RecordSyncConflictRequest struct {
	Conflict SyncConflict
	Metadata map[string]any
}

type RecordSyncConflictResult struct {
	Conflict SyncConflict
}

type MappingSpecStore interface {
	CreateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error)
	UpdateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error)
	SetStatus(
		ctx context.Context,
		providerID string,
		scope ScopeRef,
		specID string,
		version int,
		status MappingSpecStatus,
		now time.Time,
	) (MappingSpec, error)
	GetVersion(
		ctx context.Context,
		providerID string,
		scope ScopeRef,
		specID string,
		version int,
	) (MappingSpec, bool, error)
	GetLatest(ctx context.Context, providerID string, scope ScopeRef, specID string) (MappingSpec, bool, error)
	ListByScope(ctx context.Context, providerID string, scope ScopeRef) ([]MappingSpec, error)
	PublishVersion(
		ctx context.Context,
		providerID string,
		scope ScopeRef,
		specID string,
		version int,
		publishedAt time.Time,
	) (MappingSpec, error)
}

type MappingSpecLifecycleService interface {
	CreateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error)
	UpdateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error)
	MarkValidated(ctx context.Context, providerID string, scope ScopeRef, specID string, version int) (MappingSpec, error)
	Publish(ctx context.Context, providerID string, scope ScopeRef, specID string, version int) (MappingSpec, error)
	GetVersion(
		ctx context.Context,
		providerID string,
		scope ScopeRef,
		specID string,
		version int,
	) (MappingSpec, bool, error)
	GetLatest(ctx context.Context, providerID string, scope ScopeRef, specID string) (MappingSpec, bool, error)
	ListByScope(ctx context.Context, providerID string, scope ScopeRef) ([]MappingSpec, error)
}

type SyncBindingStore interface {
	Upsert(ctx context.Context, binding SyncBinding) (SyncBinding, error)
	Get(ctx context.Context, id string) (SyncBinding, error)
	ListByConnection(ctx context.Context, connectionID string) ([]SyncBinding, error)
	UpdateStatus(ctx context.Context, id string, status SyncBindingStatus, reason string) error
}

type IdentityBindingStore interface {
	Upsert(ctx context.Context, binding IdentityBinding) (IdentityBinding, error)
	GetByExternalID(
		ctx context.Context,
		syncBindingID string,
		externalID string,
	) (IdentityBinding, bool, error)
	ListByInternalID(
		ctx context.Context,
		syncBindingID string,
		internalType string,
		internalID string,
	) ([]IdentityBinding, error)
}

type IdentityReconciler interface {
	ReconcileIdentity(ctx context.Context, req ReconcileIdentityRequest) (ReconcileIdentityResult, error)
}

type SyncConflictStore interface {
	Append(ctx context.Context, conflict SyncConflict) (SyncConflict, error)
	Get(ctx context.Context, providerID string, scope ScopeRef, id string) (SyncConflict, error)
	ListByBinding(
		ctx context.Context,
		providerID string,
		scope ScopeRef,
		syncBindingID string,
		status SyncConflictStatus,
	) ([]SyncConflict, error)
	Resolve(
		ctx context.Context,
		providerID string,
		scope ScopeRef,
		id string,
		resolution SyncConflictResolution,
		resolvedAt time.Time,
	) (SyncConflict, error)
}

type SyncConflictPolicyHook interface {
	ApplyRecordPolicy(ctx context.Context, conflict SyncConflict) (SyncConflict, error)
	ApplyResolutionPolicy(
		ctx context.Context,
		conflict SyncConflict,
		resolution SyncConflictResolution,
	) (SyncConflictResolution, error)
}

type SyncConflictRecorder interface {
	RecordSyncConflict(ctx context.Context, req RecordSyncConflictRequest) (RecordSyncConflictResult, error)
}

type SyncCheckpointStore interface {
	Save(ctx context.Context, checkpoint SyncCheckpoint) (SyncCheckpoint, error)
	GetByID(ctx context.Context, providerID string, scope ScopeRef, id string) (SyncCheckpoint, bool, error)
	GetLatest(
		ctx context.Context,
		providerID string,
		scope ScopeRef,
		syncBindingID string,
		direction SyncDirection,
	) (SyncCheckpoint, bool, error)
}

type SyncChangeLogEntry struct {
	ID             string
	ProviderID     string
	Scope          ScopeRef
	ConnectionID   string
	SyncBindingID  string
	Direction      SyncDirection
	SourceObject   string
	ExternalID     string
	SourceVersion  string
	IdempotencyKey string
	Payload        map[string]any
	Metadata       map[string]any
	OccurredAt     time.Time
}

type SyncChangeLogStore interface {
	Append(ctx context.Context, entry SyncChangeLogEntry) (bool, error)
	ListSince(
		ctx context.Context,
		syncBindingID string,
		direction SyncDirection,
		cursor string,
		limit int,
	) ([]SyncChangeLogEntry, string, error)
}

type MappingSpecValidator interface {
	ValidateMappingSpec(
		ctx context.Context,
		req ValidateMappingSpecRequest,
	) (ValidateMappingSpecResult, error)
}

type MappingSpecCompiler interface {
	CompileMappingSpec(
		ctx context.Context,
		req ValidateMappingSpecRequest,
	) (CompiledMappingSpec, []MappingValidationIssue, error)
}

type MappingSpecPreviewer interface {
	PreviewMappingSpec(
		ctx context.Context,
		req PreviewMappingSpecRequest,
	) (PreviewMappingSpecResult, error)
}

type SyncPlanner interface {
	PlanSyncRun(ctx context.Context, req PlanSyncRunRequest) (SyncRunPlan, error)
}

type SyncRunner interface {
	RunSyncImport(ctx context.Context, req RunSyncImportRequest) (SyncRunResult, error)
	RunSyncExport(ctx context.Context, req RunSyncExportRequest) (SyncRunResult, error)
}

type SyncConflictResolver interface {
	ResolveSyncConflict(
		ctx context.Context,
		req ResolveSyncConflictRequest,
	) (ResolveSyncConflictResult, error)
}

type SyncConflictService interface {
	SyncConflictRecorder
	SyncConflictResolver
}

type ExternalFormDataService interface {
	MappingSpecValidator
	MappingSpecPreviewer
	SyncPlanner
	SyncRunner
	SyncConflictResolver
}
