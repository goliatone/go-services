package core

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) UpsertInstallation(ctx context.Context, in UpsertInstallationInput) (Installation, error) {
	if s == nil || s.installationStore == nil {
		return Installation{}, s.mapError(fmt.Errorf("core: installation store is required"))
	}

	in.ProviderID = strings.TrimSpace(in.ProviderID)
	in.Scope = ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(in.Scope.Type)),
		ID:   strings.TrimSpace(in.Scope.ID),
	}
	in.InstallType = strings.TrimSpace(strings.ToLower(in.InstallType))
	if in.ProviderID == "" {
		return Installation{}, s.mapError(fmt.Errorf("core: provider id is required"))
	}
	if err := in.Scope.Validate(); err != nil {
		return Installation{}, s.mapError(err)
	}
	if in.InstallType == "" {
		return Installation{}, s.mapError(fmt.Errorf("core: install type is required"))
	}
	if strings.TrimSpace(string(in.Status)) == "" {
		in.Status = InstallationStatusActive
	}

	record, err := s.installationStore.Upsert(ctx, in)
	if err != nil {
		return Installation{}, s.mapError(err)
	}
	return record, nil
}

func (s *Service) GetInstallation(ctx context.Context, id string) (Installation, error) {
	if s == nil || s.installationStore == nil {
		return Installation{}, s.mapError(fmt.Errorf("core: installation store is required"))
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Installation{}, s.mapError(fmt.Errorf("core: installation id is required"))
	}
	record, err := s.installationStore.Get(ctx, id)
	if err != nil {
		return Installation{}, s.mapError(err)
	}
	return record, nil
}

func (s *Service) ListInstallations(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
) ([]Installation, error) {
	if s == nil || s.installationStore == nil {
		return nil, s.mapError(fmt.Errorf("core: installation store is required"))
	}
	providerID = strings.TrimSpace(providerID)
	scope = ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(scope.Type)),
		ID:   strings.TrimSpace(scope.ID),
	}
	if providerID == "" {
		return nil, s.mapError(fmt.Errorf("core: provider id is required"))
	}
	if err := scope.Validate(); err != nil {
		return nil, s.mapError(err)
	}
	items, err := s.installationStore.ListByScope(ctx, providerID, scope)
	if err != nil {
		return nil, s.mapError(err)
	}
	return items, nil
}

func (s *Service) UpdateInstallationStatus(
	ctx context.Context,
	id string,
	status string,
	reason string,
) error {
	if s == nil || s.installationStore == nil {
		return s.mapError(fmt.Errorf("core: installation store is required"))
	}
	id = strings.TrimSpace(id)
	status = strings.TrimSpace(status)
	if id == "" || status == "" {
		return s.mapError(fmt.Errorf("core: installation id and status are required"))
	}
	if err := s.installationStore.UpdateStatus(ctx, id, status, strings.TrimSpace(reason)); err != nil {
		return s.mapError(err)
	}
	return nil
}
