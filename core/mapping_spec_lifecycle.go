package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type MappingSpecLifecycleOption func(*MappingSpecLifecycle)

func WithMappingSpecLifecycleClock(now func() time.Time) MappingSpecLifecycleOption {
	return func(s *MappingSpecLifecycle) {
		if s == nil || now == nil {
			return
		}
		s.now = now
	}
}

func WithMappingSpecLifecycleEventBus(bus LifecycleEventBus) MappingSpecLifecycleOption {
	return func(s *MappingSpecLifecycle) {
		if s == nil {
			return
		}
		s.eventBus = bus
	}
}

type MappingSpecLifecycle struct {
	store    MappingSpecStore
	eventBus LifecycleEventBus
	now      func() time.Time
}

func NewMappingSpecLifecycle(
	store MappingSpecStore,
	opts ...MappingSpecLifecycleOption,
) (*MappingSpecLifecycle, error) {
	if store == nil {
		return nil, fmt.Errorf("core: mapping spec store is required")
	}
	svc := &MappingSpecLifecycle{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc, nil
}

func (s *MappingSpecLifecycle) CreateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error) {
	if s == nil || s.store == nil {
		return MappingSpec{}, fmt.Errorf("core: mapping spec lifecycle store is required")
	}
	spec = normalizeMappingSpec(spec)
	spec.Status = MappingSpecStatusDraft
	spec.PublishedAt = nil

	if spec.Version == 0 {
		latest, found, err := s.store.GetLatest(ctx, spec.ProviderID, spec.Scope, spec.SpecID)
		if err != nil {
			return MappingSpec{}, err
		}
		if found {
			if latest.Status != MappingSpecStatusPublished {
				return MappingSpec{}, fmt.Errorf(
					"core: latest mapping spec version must be published before creating a new draft",
				)
			}
			spec.Version = latest.Version + 1
		} else {
			spec.Version = 1
		}
	}

	if existing, found, err := s.store.GetVersion(ctx, spec.ProviderID, spec.Scope, spec.SpecID, spec.Version); err != nil {
		return MappingSpec{}, err
	} else if found {
		if existing.Status == MappingSpecStatusPublished {
			return MappingSpec{}, fmt.Errorf(
				"core: mapping spec %s version %d is immutable once published",
				spec.SpecID,
				spec.Version,
			)
		}
		return MappingSpec{}, fmt.Errorf(
			"core: mapping spec %s version %d already exists",
			spec.SpecID,
			spec.Version,
		)
	}

	if err := spec.Validate(); err != nil {
		return MappingSpec{}, err
	}
	saved, err := s.store.CreateDraft(ctx, spec)
	if err != nil {
		return MappingSpec{}, err
	}
	if publishErr := s.publishMappingSpecEvent(ctx, saved, "services.mapping_spec.draft_created"); publishErr != nil {
		return MappingSpec{}, publishErr
	}
	return saved, nil
}

func (s *MappingSpecLifecycle) UpdateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error) {
	if s == nil || s.store == nil {
		return MappingSpec{}, fmt.Errorf("core: mapping spec lifecycle store is required")
	}
	spec = normalizeMappingSpec(spec)
	if spec.Version < 1 {
		return MappingSpec{}, fmt.Errorf("core: mapping spec version must be >= 1")
	}

	existing, found, err := s.store.GetVersion(ctx, spec.ProviderID, spec.Scope, spec.SpecID, spec.Version)
	if err != nil {
		return MappingSpec{}, err
	}
	if !found {
		return MappingSpec{}, fmt.Errorf(
			"core: mapping spec %s version %d not found",
			spec.SpecID,
			spec.Version,
		)
	}
	if existing.Status == MappingSpecStatusPublished {
		return MappingSpec{}, fmt.Errorf(
			"core: mapping spec %s version %d is immutable once published",
			spec.SpecID,
			spec.Version,
		)
	}

	spec.Status = MappingSpecStatusDraft
	spec.PublishedAt = nil
	if err := spec.Validate(); err != nil {
		return MappingSpec{}, err
	}
	updated, err := s.store.UpdateDraft(ctx, spec)
	if err != nil {
		return MappingSpec{}, err
	}
	if publishErr := s.publishMappingSpecEvent(ctx, updated, "services.mapping_spec.draft_updated"); publishErr != nil {
		return MappingSpec{}, publishErr
	}
	return updated, nil
}

func (s *MappingSpecLifecycle) MarkValidated(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
	version int,
) (MappingSpec, error) {
	if s == nil || s.store == nil {
		return MappingSpec{}, fmt.Errorf("core: mapping spec lifecycle store is required")
	}
	providerID, scope, err := normalizeProviderScope(providerID, scope)
	if err != nil {
		return MappingSpec{}, err
	}
	specID = strings.TrimSpace(specID)
	if specID == "" || version < 1 {
		return MappingSpec{}, fmt.Errorf("core: mapping spec id and version are required")
	}

	current, found, err := s.store.GetVersion(ctx, providerID, scope, specID, version)
	if err != nil {
		return MappingSpec{}, err
	}
	if !found {
		return MappingSpec{}, fmt.Errorf(
			"core: mapping spec %s version %d not found for provider/scope",
			specID,
			version,
		)
	}
	if !sameProviderScope(current.ProviderID, current.Scope, providerID, scope) {
		return MappingSpec{}, fmt.Errorf("core: mapping spec scope mismatch")
	}
	if current.Status == MappingSpecStatusPublished {
		return MappingSpec{}, fmt.Errorf(
			"core: mapping spec %s version %d is already published and immutable",
			specID,
			version,
		)
	}
	if current.Status != MappingSpecStatusDraft && current.Status != MappingSpecStatusValidated {
		return MappingSpec{}, fmt.Errorf(
			"core: mapping spec %s version %d cannot transition to validated from %s",
			specID,
			version,
			current.Status,
		)
	}

	validated, err := s.store.SetStatus(
		ctx,
		providerID,
		scope,
		specID,
		version,
		MappingSpecStatusValidated,
		s.now(),
	)
	if err != nil {
		return MappingSpec{}, err
	}
	if publishErr := s.publishMappingSpecEvent(ctx, validated, "services.mapping_spec.validated"); publishErr != nil {
		return MappingSpec{}, publishErr
	}
	return validated, nil
}

func (s *MappingSpecLifecycle) Publish(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
	version int,
) (MappingSpec, error) {
	if s == nil || s.store == nil {
		return MappingSpec{}, fmt.Errorf("core: mapping spec lifecycle store is required")
	}
	providerID, scope, err := normalizeProviderScope(providerID, scope)
	if err != nil {
		return MappingSpec{}, err
	}
	specID = strings.TrimSpace(specID)
	if specID == "" || version < 1 {
		return MappingSpec{}, fmt.Errorf("core: mapping spec id and version are required")
	}

	current, found, err := s.store.GetVersion(ctx, providerID, scope, specID, version)
	if err != nil {
		return MappingSpec{}, err
	}
	if !found {
		return MappingSpec{}, fmt.Errorf(
			"core: mapping spec %s version %d not found for provider/scope",
			specID,
			version,
		)
	}
	if !sameProviderScope(current.ProviderID, current.Scope, providerID, scope) {
		return MappingSpec{}, fmt.Errorf("core: mapping spec scope mismatch")
	}
	if current.Status == MappingSpecStatusPublished {
		return current, nil
	}
	if current.Status != MappingSpecStatusValidated {
		return MappingSpec{}, fmt.Errorf(
			"core: mapping spec %s version %d must be validated before publish",
			specID,
			version,
		)
	}

	published, err := s.store.PublishVersion(ctx, providerID, scope, specID, version, s.now())
	if err != nil {
		return MappingSpec{}, err
	}
	if publishErr := s.publishMappingSpecEvent(ctx, published, "services.mapping_spec.published"); publishErr != nil {
		return MappingSpec{}, publishErr
	}
	return published, nil
}

func (s *MappingSpecLifecycle) GetVersion(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
	version int,
) (MappingSpec, bool, error) {
	if s == nil || s.store == nil {
		return MappingSpec{}, false, fmt.Errorf("core: mapping spec lifecycle store is required")
	}
	providerID, scope, err := normalizeProviderScope(providerID, scope)
	if err != nil {
		return MappingSpec{}, false, err
	}
	specID = strings.TrimSpace(specID)
	if specID == "" || version < 1 {
		return MappingSpec{}, false, fmt.Errorf("core: mapping spec id and version are required")
	}
	return s.store.GetVersion(ctx, providerID, scope, specID, version)
}

func (s *MappingSpecLifecycle) GetLatest(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
) (MappingSpec, bool, error) {
	if s == nil || s.store == nil {
		return MappingSpec{}, false, fmt.Errorf("core: mapping spec lifecycle store is required")
	}
	providerID, scope, err := normalizeProviderScope(providerID, scope)
	if err != nil {
		return MappingSpec{}, false, err
	}
	specID = strings.TrimSpace(specID)
	if specID == "" {
		return MappingSpec{}, false, fmt.Errorf("core: mapping spec id is required")
	}
	return s.store.GetLatest(ctx, providerID, scope, specID)
}

func (s *MappingSpecLifecycle) ListByScope(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
) ([]MappingSpec, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("core: mapping spec lifecycle store is required")
	}
	providerID = strings.TrimSpace(providerID)
	scope = ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(scope.Type)),
		ID:   strings.TrimSpace(scope.ID),
	}
	if providerID == "" {
		return nil, fmt.Errorf("core: provider id is required")
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return s.store.ListByScope(ctx, providerID, scope)
}

func normalizeMappingSpec(spec MappingSpec) MappingSpec {
	spec.ID = strings.TrimSpace(spec.ID)
	spec.SpecID = strings.TrimSpace(spec.SpecID)
	spec.ProviderID = strings.TrimSpace(spec.ProviderID)
	spec.Scope = ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(spec.Scope.Type)),
		ID:   strings.TrimSpace(spec.Scope.ID),
	}
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Description = strings.TrimSpace(spec.Description)
	spec.SourceObject = strings.TrimSpace(spec.SourceObject)
	spec.TargetModel = strings.TrimSpace(spec.TargetModel)
	spec.SchemaRef = strings.TrimSpace(spec.SchemaRef)
	for idx := range spec.Rules {
		spec.Rules[idx].ID = strings.TrimSpace(spec.Rules[idx].ID)
		spec.Rules[idx].SourcePath = strings.TrimSpace(spec.Rules[idx].SourcePath)
		spec.Rules[idx].TargetPath = strings.TrimSpace(spec.Rules[idx].TargetPath)
		spec.Rules[idx].Transform = strings.TrimSpace(spec.Rules[idx].Transform)
	}
	return spec
}

func normalizeProviderScope(providerID string, scope ScopeRef) (string, ScopeRef, error) {
	providerID = strings.TrimSpace(providerID)
	scope = ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(scope.Type)),
		ID:   strings.TrimSpace(scope.ID),
	}
	if providerID == "" {
		return "", ScopeRef{}, fmt.Errorf("core: provider id is required")
	}
	if err := scope.Validate(); err != nil {
		return "", ScopeRef{}, err
	}
	return providerID, scope, nil
}

func sameProviderScope(
	actualProviderID string,
	actualScope ScopeRef,
	expectedProviderID string,
	expectedScope ScopeRef,
) bool {
	if !strings.EqualFold(strings.TrimSpace(actualProviderID), strings.TrimSpace(expectedProviderID)) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(actualScope.Type), strings.TrimSpace(expectedScope.Type)) &&
		strings.TrimSpace(actualScope.ID) == strings.TrimSpace(expectedScope.ID)
}

func (s *MappingSpecLifecycle) publishMappingSpecEvent(
	ctx context.Context,
	spec MappingSpec,
	eventName string,
) error {
	if s == nil || s.eventBus == nil {
		return nil
	}
	occurredAt := s.now()
	event := LifecycleEvent{
		ID:           buildMappingSpecLifecycleEventID(spec.SpecID, spec.Version, eventName, occurredAt),
		Name:         eventName,
		ProviderID:   spec.ProviderID,
		ScopeType:    spec.Scope.Type,
		ScopeID:      spec.Scope.ID,
		ConnectionID: "",
		Source:       "services.mapping_specs",
		OccurredAt:   occurredAt,
		Payload: map[string]any{
			"spec_id":     spec.SpecID,
			"version":     spec.Version,
			"status":      string(spec.Status),
			"name":        spec.Name,
			"schema_ref":  spec.SchemaRef,
			"provider_id": spec.ProviderID,
		},
		Metadata: copyMetadata(spec.Metadata),
	}
	return s.eventBus.Publish(ctx, event)
}

func buildMappingSpecLifecycleEventID(
	specID string,
	version int,
	eventName string,
	occurredAt time.Time,
) string {
	payload := strings.Join(
		[]string{
			strings.TrimSpace(specID),
			fmt.Sprintf("%d", version),
			strings.TrimSpace(eventName),
			occurredAt.UTC().Format(time.RFC3339Nano),
		},
		"|",
	)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}
