package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-services/core"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type InstallationStore struct {
	db   *bun.DB
	repo repository.Repository[*installationRecord]
}

func NewInstallationStore(db *bun.DB) (*InstallationStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepository[*installationRecord](db, installationHandlers())
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid installation repository wiring: %w", err)
		}
	}
	return &InstallationStore{
		db:   db,
		repo: repo,
	}, nil
}

func (s *InstallationStore) Upsert(ctx context.Context, in core.UpsertInstallationInput) (core.Installation, error) {
	if s == nil || s.db == nil || s.repo == nil {
		return core.Installation{}, fmt.Errorf("sqlstore: installation store is not configured")
	}
	in = normalizeInstallationInput(in)
	status, err := validateInstallationInput(in)
	if err != nil {
		return core.Installation{}, err
	}
	in.Status = status

	now := time.Now().UTC()
	return runInTxResult(ctx, s.db, func(ctx context.Context, tx bun.Tx) (core.Installation, error) {
		return upsertInstallationTx(ctx, tx, in, status, now)
	})
}

func normalizeInstallationInput(in core.UpsertInstallationInput) core.UpsertInstallationInput {
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	in.Scope = core.ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(in.Scope.Type)),
		ID:   strings.TrimSpace(in.Scope.ID),
	}
	in.InstallType = strings.TrimSpace(strings.ToLower(in.InstallType))
	return in
}

func validateInstallationInput(in core.UpsertInstallationInput) (core.InstallationStatus, error) {
	if in.ProviderID == "" {
		return "", fmt.Errorf("sqlstore: provider id is required")
	}
	if err := in.Scope.Validate(); err != nil {
		return "", err
	}
	if in.InstallType == "" {
		return "", fmt.Errorf("sqlstore: install type is required")
	}
	return parseInstallationStatusValue(in.Status)
}

func upsertInstallationTx(
	ctx context.Context,
	tx bun.Tx,
	in core.UpsertInstallationInput,
	status core.InstallationStatus,
	now time.Time,
) (core.Installation, error) {
	record, err := findInstallationTx(ctx, tx, in.ProviderID, in.Scope.Type, in.Scope.ID, in.InstallType)
	if err != nil {
		return core.Installation{}, err
	}
	if record == nil {
		if status != core.InstallationStatusActive {
			return core.Installation{}, fmt.Errorf("sqlstore: installation must be created with status active")
		}
		record = newInstallationRecord(in, now)
		record.ID = uuid.NewString()
		_, err = tx.NewInsert().Model(record).Exec(ctx)
		return record.toDomain(), err
	}
	candidate := record.toDomain()
	if transitionErr := candidate.TransitionTo(status, now); transitionErr != nil {
		return core.Installation{}, transitionErr
	}
	updateInstallationRecord(record, in, status, now)
	_, err = tx.NewUpdate().Model(record).Where("id = ?", record.ID).Exec(ctx)
	return record.toDomain(), err
}

func updateInstallationRecord(
	record *installationRecord,
	in core.UpsertInstallationInput,
	status core.InstallationStatus,
	now time.Time,
) {
	record.Status = string(status)
	record.Metadata = copyAnyMap(in.Metadata)
	record.UpdatedAt = now
	if in.GrantedAt != nil {
		value := *in.GrantedAt
		record.GrantedAt = &value
	}
	if in.RevokedAt != nil {
		value := *in.RevokedAt
		record.RevokedAt = &value
	}
	if status == core.InstallationStatusUninstalled && record.RevokedAt == nil {
		record.RevokedAt = &now
	}
}

func (s *InstallationStore) Get(ctx context.Context, id string) (core.Installation, error) {
	if s == nil || s.repo == nil {
		return core.Installation{}, fmt.Errorf("sqlstore: installation store is not configured")
	}
	record, err := s.repo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return core.Installation{}, err
	}
	return record.toDomain(), nil
}

func (s *InstallationStore) ListByScope(
	ctx context.Context,
	providerID string,
	scope core.ScopeRef,
) ([]core.Installation, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("sqlstore: installation store is not configured")
	}
	providerID = strings.TrimSpace(providerID)
	scope = core.ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(scope.Type)),
		ID:   strings.TrimSpace(scope.ID),
	}
	if providerID == "" {
		return nil, fmt.Errorf("sqlstore: provider id is required")
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	records, _, err := s.repo.List(ctx,
		repository.SelectBy("provider_id", "=", providerID),
		repository.SelectBy("scope_type", "=", scope.Type),
		repository.SelectBy("scope_id", "=", scope.ID),
		repository.OrderBy("updated_at DESC"),
	)
	if err != nil {
		return nil, err
	}
	out := make([]core.Installation, 0, len(records))
	for _, record := range records {
		out = append(out, record.toDomain())
	}
	return out, nil
}

func (s *InstallationStore) UpdateStatus(
	ctx context.Context,
	id string,
	status core.InstallationStatus,
	reason string,
) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: installation store is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" || status == "" {
		return fmt.Errorf("sqlstore: installation id and status are required")
	}
	targetStatus, parseErr := parseInstallationStatusValue(status)
	if parseErr != nil {
		return parseErr
	}

	record, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	candidate := record.toDomain()
	if transitionErr := candidate.TransitionTo(targetStatus, now); transitionErr != nil {
		return transitionErr
	}
	record.Status = string(targetStatus)
	record.UpdatedAt = now
	record.Metadata = copyAnyMap(record.Metadata)
	if strings.TrimSpace(reason) != "" {
		record.Metadata["status_reason"] = strings.TrimSpace(reason)
		record.Metadata["status_reason_at"] = now.Format(time.RFC3339Nano)
	}
	switch targetStatus {
	case core.InstallationStatusUninstalled:
		record.RevokedAt = &now
	}
	_, err = s.repo.Update(ctx, record, repository.UpdateByID(id))
	return err
}

func findInstallationTx(
	ctx context.Context,
	tx bun.Tx,
	providerID string,
	scopeType string,
	scopeID string,
	installType string,
) (*installationRecord, error) {
	record := &installationRecord{}
	err := tx.NewSelect().
		Model(record).
		Where("?TableAlias.provider_id = ?", strings.TrimSpace(providerID)).
		Where("?TableAlias.scope_type = ?", strings.TrimSpace(scopeType)).
		Where("?TableAlias.scope_id = ?", strings.TrimSpace(scopeID)).
		Where("?TableAlias.install_type = ?", strings.TrimSpace(installType)).
		OrderExpr("?TableAlias.updated_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
}

func parseInstallationStatusValue(status core.InstallationStatus) (core.InstallationStatus, error) {
	normalized := core.InstallationStatus(strings.TrimSpace(strings.ToLower(string(status))))
	if normalized == "" {
		return core.InstallationStatusActive, nil
	}
	switch normalized {
	case core.InstallationStatusActive,
		core.InstallationStatusSuspended,
		core.InstallationStatusUninstalled,
		core.InstallationStatusNeedsReconsent:
		return normalized, nil
	default:
		return "", fmt.Errorf("sqlstore: invalid installation status %q", string(status))
	}
}
