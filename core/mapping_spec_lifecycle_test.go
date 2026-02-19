package core

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

type inMemoryMappingSpecStore struct {
	records map[string]map[int]MappingSpec
}

func newInMemoryMappingSpecStore() *inMemoryMappingSpecStore {
	return &inMemoryMappingSpecStore{
		records: make(map[string]map[int]MappingSpec),
	}
}

func (s *inMemoryMappingSpecStore) CreateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error) {
	if _, ok := s.records[spec.SpecID]; !ok {
		s.records[spec.SpecID] = make(map[int]MappingSpec)
	}
	if _, exists := s.records[spec.SpecID][spec.Version]; exists {
		return MappingSpec{}, fmt.Errorf("duplicate version")
	}
	s.records[spec.SpecID][spec.Version] = spec
	return spec, nil
}

func (s *inMemoryMappingSpecStore) UpdateDraft(ctx context.Context, spec MappingSpec) (MappingSpec, error) {
	if _, ok := s.records[spec.SpecID]; !ok {
		return MappingSpec{}, fmt.Errorf("missing spec")
	}
	if _, ok := s.records[spec.SpecID][spec.Version]; !ok {
		return MappingSpec{}, fmt.Errorf("missing version")
	}
	s.records[spec.SpecID][spec.Version] = spec
	return spec, nil
}

func (s *inMemoryMappingSpecStore) SetStatus(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
	version int,
	status MappingSpecStatus,
	now time.Time,
) (MappingSpec, error) {
	spec, found, err := s.GetVersion(ctx, providerID, scope, specID, version)
	if err != nil {
		return MappingSpec{}, err
	}
	if !found {
		return MappingSpec{}, fmt.Errorf("missing version")
	}
	spec.Status = status
	spec.UpdatedAt = now
	if status != MappingSpecStatusPublished {
		spec.PublishedAt = nil
	}
	s.records[specID][version] = spec
	return spec, nil
}

func (s *inMemoryMappingSpecStore) GetVersion(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
	version int,
) (MappingSpec, bool, error) {
	versions, ok := s.records[specID]
	if !ok {
		return MappingSpec{}, false, nil
	}
	spec, ok := versions[version]
	if !ok {
		return MappingSpec{}, false, nil
	}
	if !sameProviderScope(spec.ProviderID, spec.Scope, providerID, scope) {
		return MappingSpec{}, false, nil
	}
	return spec, true, nil
}

func (s *inMemoryMappingSpecStore) GetLatest(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
) (MappingSpec, bool, error) {
	versions, ok := s.records[specID]
	if !ok || len(versions) == 0 {
		return MappingSpec{}, false, nil
	}
	keys := make([]int, 0, len(versions))
	for version := range versions {
		keys = append(keys, version)
	}
	slices.Sort(keys)
	latest := versions[keys[len(keys)-1]]
	if !sameProviderScope(latest.ProviderID, latest.Scope, providerID, scope) {
		return MappingSpec{}, false, nil
	}
	return latest, true, nil
}

func (s *inMemoryMappingSpecStore) ListByScope(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
) ([]MappingSpec, error) {
	var out []MappingSpec
	for _, versions := range s.records {
		for _, spec := range versions {
			if spec.ProviderID == providerID &&
				spec.Scope.Type == scope.Type &&
				spec.Scope.ID == scope.ID {
				out = append(out, spec)
			}
		}
	}
	return out, nil
}

func (s *inMemoryMappingSpecStore) PublishVersion(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	specID string,
	version int,
	publishedAt time.Time,
) (MappingSpec, error) {
	spec, found, err := s.GetVersion(ctx, providerID, scope, specID, version)
	if err != nil {
		return MappingSpec{}, err
	}
	if !found {
		return MappingSpec{}, fmt.Errorf("missing version")
	}
	spec.Status = MappingSpecStatusPublished
	spec.PublishedAt = &publishedAt
	spec.UpdatedAt = publishedAt
	s.records[specID][version] = spec
	return spec, nil
}

func TestMappingSpecLifecycleDraftValidatePublishAndVersioning(t *testing.T) {
	store := newInMemoryMappingSpecStore()
	svc, err := NewMappingSpecLifecycle(store, WithMappingSpecLifecycleClock(func() time.Time {
		return time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	}))
	if err != nil {
		t.Fatalf("new mapping spec lifecycle: %v", err)
	}

	spec, err := svc.CreateDraft(context.Background(), MappingSpec{
		SpecID:       "spec_contacts",
		ProviderID:   "hubspot",
		Scope:        ScopeRef{Type: "org", ID: "org_123"},
		Name:         "contacts",
		SourceObject: "contacts",
		TargetModel:  "crm_contacts",
	})
	if err != nil {
		t.Fatalf("create draft v1: %v", err)
	}
	if spec.Version != 1 || spec.Status != MappingSpecStatusDraft {
		t.Fatalf("expected draft v1, got version=%d status=%s", spec.Version, spec.Status)
	}

	spec, err = svc.MarkValidated(
		context.Background(),
		"hubspot",
		ScopeRef{Type: "org", ID: "org_123"},
		"spec_contacts",
		1,
	)
	if err != nil {
		t.Fatalf("mark validated: %v", err)
	}
	if spec.Status != MappingSpecStatusValidated {
		t.Fatalf("expected validated status, got %s", spec.Status)
	}

	spec, err = svc.Publish(
		context.Background(),
		"hubspot",
		ScopeRef{Type: "org", ID: "org_123"},
		"spec_contacts",
		1,
	)
	if err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	if spec.Status != MappingSpecStatusPublished || spec.PublishedAt == nil {
		t.Fatalf("expected published v1 with published_at")
	}

	spec, err = svc.CreateDraft(context.Background(), MappingSpec{
		SpecID:       "spec_contacts",
		ProviderID:   "hubspot",
		Scope:        ScopeRef{Type: "org", ID: "org_123"},
		Name:         "contacts",
		SourceObject: "contacts",
		TargetModel:  "crm_contacts",
	})
	if err != nil {
		t.Fatalf("create draft v2: %v", err)
	}
	if spec.Version != 2 || spec.Status != MappingSpecStatusDraft {
		t.Fatalf("expected draft v2, got version=%d status=%s", spec.Version, spec.Status)
	}
}

func TestMappingSpecLifecycleCreateDraftRequiresPublishedLatest(t *testing.T) {
	store := newInMemoryMappingSpecStore()
	svc, err := NewMappingSpecLifecycle(store)
	if err != nil {
		t.Fatalf("new mapping spec lifecycle: %v", err)
	}

	_, err = svc.CreateDraft(context.Background(), MappingSpec{
		SpecID:       "spec_contacts",
		ProviderID:   "hubspot",
		Scope:        ScopeRef{Type: "org", ID: "org_123"},
		Name:         "contacts",
		SourceObject: "contacts",
		TargetModel:  "crm_contacts",
	})
	if err != nil {
		t.Fatalf("create first draft: %v", err)
	}

	_, err = svc.CreateDraft(context.Background(), MappingSpec{
		SpecID:       "spec_contacts",
		ProviderID:   "hubspot",
		Scope:        ScopeRef{Type: "org", ID: "org_123"},
		Name:         "contacts",
		SourceObject: "contacts",
		TargetModel:  "crm_contacts",
	})
	if err == nil {
		t.Fatalf("expected create draft to fail when latest version is not published")
	}
}

func TestMappingSpecLifecycleUpdateDraftRejectsPublishedVersion(t *testing.T) {
	store := newInMemoryMappingSpecStore()
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	svc, err := NewMappingSpecLifecycle(store, WithMappingSpecLifecycleClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("new mapping spec lifecycle: %v", err)
	}

	_, err = svc.CreateDraft(context.Background(), MappingSpec{
		SpecID:       "spec_contacts",
		ProviderID:   "hubspot",
		Scope:        ScopeRef{Type: "org", ID: "org_123"},
		Name:         "contacts",
		SourceObject: "contacts",
		TargetModel:  "crm_contacts",
	})
	if err != nil {
		t.Fatalf("create first draft: %v", err)
	}
	if _, err := svc.MarkValidated(
		context.Background(),
		"hubspot",
		ScopeRef{Type: "org", ID: "org_123"},
		"spec_contacts",
		1,
	); err != nil {
		t.Fatalf("mark validated: %v", err)
	}
	if _, err := svc.Publish(
		context.Background(),
		"hubspot",
		ScopeRef{Type: "org", ID: "org_123"},
		"spec_contacts",
		1,
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	_, err = svc.UpdateDraft(context.Background(), MappingSpec{
		SpecID:       "spec_contacts",
		ProviderID:   "hubspot",
		Scope:        ScopeRef{Type: "org", ID: "org_123"},
		Name:         "contacts v1 edited",
		SourceObject: "contacts",
		TargetModel:  "crm_contacts",
		Version:      1,
	})
	if err == nil {
		t.Fatalf("expected update draft to fail for published version")
	}
}

func TestMappingSpecLifecycleEmitsAuditEvents(t *testing.T) {
	store := newInMemoryMappingSpecStore()
	eventBus := &recordingLifecycleEventBus{}
	svc, err := NewMappingSpecLifecycle(
		store,
		WithMappingSpecLifecycleEventBus(eventBus),
		WithMappingSpecLifecycleClock(func() time.Time {
			return time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
		}),
	)
	if err != nil {
		t.Fatalf("new mapping spec lifecycle: %v", err)
	}

	spec, err := svc.CreateDraft(context.Background(), MappingSpec{
		SpecID:       "spec_contacts",
		ProviderID:   "hubspot",
		Scope:        ScopeRef{Type: "org", ID: "org_123"},
		Name:         "contacts",
		SourceObject: "contacts",
		TargetModel:  "crm_contacts",
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := svc.MarkValidated(
		context.Background(),
		spec.ProviderID,
		spec.Scope,
		spec.SpecID,
		spec.Version,
	); err != nil {
		t.Fatalf("mark validated: %v", err)
	}
	if _, err := svc.Publish(
		context.Background(),
		spec.ProviderID,
		spec.Scope,
		spec.SpecID,
		spec.Version,
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(eventBus.events) != 3 {
		t.Fatalf("expected 3 lifecycle events, got %d", len(eventBus.events))
	}
	if eventBus.events[0].Name != "services.mapping_spec.draft_created" {
		t.Fatalf("unexpected first event name %q", eventBus.events[0].Name)
	}
	if eventBus.events[1].Name != "services.mapping_spec.validated" {
		t.Fatalf("unexpected second event name %q", eventBus.events[1].Name)
	}
	if eventBus.events[2].Name != "services.mapping_spec.published" {
		t.Fatalf("unexpected third event name %q", eventBus.events[2].Name)
	}
}

func TestMappingSpecLifecycleMarkValidatedFailsClosedOnScopeMismatch(t *testing.T) {
	store := newInMemoryMappingSpecStore()
	svc, err := NewMappingSpecLifecycle(store)
	if err != nil {
		t.Fatalf("new mapping spec lifecycle: %v", err)
	}

	if _, err := svc.CreateDraft(context.Background(), MappingSpec{
		SpecID:       "spec_contacts",
		ProviderID:   "hubspot",
		Scope:        ScopeRef{Type: "org", ID: "org_123"},
		Name:         "contacts",
		SourceObject: "contacts",
		TargetModel:  "crm_contacts",
	}); err != nil {
		t.Fatalf("create draft: %v", err)
	}

	_, err = svc.MarkValidated(
		context.Background(),
		"hubspot",
		ScopeRef{Type: "org", ID: "org_999"},
		"spec_contacts",
		1,
	)
	if err == nil {
		t.Fatalf("expected mark validated to fail for mismatched scope")
	}
}
