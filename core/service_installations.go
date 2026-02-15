package core

import (
	"context"
	"fmt"
	"strings"
	"time"
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
	status, parseErr := parseInstallationStatus(in.Status)
	if parseErr != nil {
		return Installation{}, s.mapError(parseErr)
	}
	in.Status = status

	existingByScope, err := s.installationStore.ListByScope(ctx, in.ProviderID, in.Scope)
	if err != nil {
		return Installation{}, s.mapError(err)
	}
	found := false
	for _, item := range existingByScope {
		if strings.EqualFold(strings.TrimSpace(item.InstallType), in.InstallType) {
			found = true
			candidate := item
			if transitionErr := candidate.TransitionTo(status, time.Now().UTC()); transitionErr != nil {
				return Installation{}, s.mapError(transitionErr)
			}
			break
		}
	}
	if !found && status != InstallationStatusActive {
		return Installation{}, s.mapError(fmt.Errorf("core: installation must be created with status active"))
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
	targetStatus, parseErr := parseInstallationStatus(InstallationStatus(status))
	if parseErr != nil {
		return s.mapError(parseErr)
	}
	current, err := s.installationStore.Get(ctx, id)
	if err != nil {
		return s.mapError(err)
	}
	candidate := current
	if transitionErr := candidate.TransitionTo(targetStatus, time.Now().UTC()); transitionErr != nil {
		return s.mapError(transitionErr)
	}
	if err := s.installationStore.UpdateStatus(ctx, id, string(targetStatus), strings.TrimSpace(reason)); err != nil {
		return s.mapError(err)
	}
	return nil
}

func parseInstallationStatus(status InstallationStatus) (InstallationStatus, error) {
	normalized := InstallationStatus(strings.TrimSpace(strings.ToLower(string(status))))
	if normalized == "" {
		return InstallationStatusActive, nil
	}
	switch normalized {
	case InstallationStatusActive,
		InstallationStatusSuspended,
		InstallationStatusUninstalled,
		InstallationStatusNeedsReconsent:
		return normalized, nil
	default:
		return "", fmt.Errorf("core: invalid installation status %q", status)
	}
}
