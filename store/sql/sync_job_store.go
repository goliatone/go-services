package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-services/core"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type SyncJobStore struct {
	db   *bun.DB
	repo repository.Repository[*syncJobRecord]
}

var errSyncJobIdempotencyReplay = errors.New("sqlstore: sync job idempotency replay")

func NewSyncJobStore(db *bun.DB) (*SyncJobStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepository[*syncJobRecord](db, syncJobHandlers())
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid sync job repository wiring: %w", err)
		}
	}
	return &SyncJobStore{
		db:   db,
		repo: repo,
	}, nil
}

func (s *SyncJobStore) Create(ctx context.Context, job core.SyncJob) (core.SyncJob, error) {
	if s == nil || s.db == nil {
		return core.SyncJob{}, fmt.Errorf("sqlstore: sync job store is not configured")
	}
	job.ConnectionID = strings.TrimSpace(job.ConnectionID)
	job.ProviderID = strings.TrimSpace(job.ProviderID)
	if job.ConnectionID == "" || job.ProviderID == "" {
		return core.SyncJob{}, fmt.Errorf("sqlstore: connection id and provider id are required")
	}
	if strings.TrimSpace(job.ID) == "" {
		job.ID = uuid.NewString()
	}
	if job.Mode == "" {
		job.Mode = core.SyncJobModeIncremental
	}
	if job.Status == "" {
		job.Status = core.SyncJobStatusQueued
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	record := newSyncJobRecord(job, now)
	record.ID = job.ID
	if _, err := s.db.NewInsert().Model(record).Exec(ctx); err != nil {
		return core.SyncJob{}, err
	}
	return record.toDomain(), nil
}

func (s *SyncJobStore) CreateSyncJob(
	ctx context.Context,
	in core.CreateSyncJobStoreInput,
) (core.CreateSyncJobResult, error) {
	if s == nil || s.db == nil {
		return core.CreateSyncJobResult{}, fmt.Errorf("sqlstore: sync job store is not configured")
	}
	normalized, err := normalizeCreateSyncJobStoreInput(in)
	if err != nil {
		return core.CreateSyncJobResult{}, err
	}

	job := core.SyncJob{
		ConnectionID: normalized.ConnectionID,
		ProviderID:   normalized.ProviderID,
		Mode:         normalized.Mode,
		Status:       core.SyncJobStatusQueued,
		Metadata:     copyAnyMap(normalized.Metadata),
	}
	if normalized.RequestedBy != "" {
		job.Metadata["requested_by"] = normalized.RequestedBy
	}
	idempotencyKey := strings.TrimSpace(normalized.IdempotencyKey)
	if idempotencyKey == "" {
		created, createErr := s.Create(ctx, job)
		if createErr != nil {
			return core.CreateSyncJobResult{}, createErr
		}
		return core.CreateSyncJobResult{
			Job:     created,
			Created: true,
		}, nil
	}

	created, createErr := s.createSyncJobWithIdempotency(ctx, normalized, job)
	if createErr == nil {
		return core.CreateSyncJobResult{
			Job:     created,
			Created: true,
		}, nil
	}
	if !errors.Is(createErr, errSyncJobIdempotencyReplay) {
		return core.CreateSyncJobResult{}, createErr
	}

	record, lookupErr := s.findSyncJobIdempotency(
		ctx,
		normalized.Scope,
		normalized.ProviderID,
		normalized.Mode,
		idempotencyKey,
	)
	if lookupErr != nil {
		return core.CreateSyncJobResult{}, lookupErr
	}
	if record == nil || strings.TrimSpace(record.SyncJobID) == "" {
		return core.CreateSyncJobResult{}, fmt.Errorf(
			"sqlstore: %w: idempotency tuple has no sync job binding",
			core.ErrSyncJobNotFound,
		)
	}
	existing, getErr := s.GetSyncJob(ctx, record.SyncJobID)
	if getErr != nil {
		return core.CreateSyncJobResult{}, getErr
	}
	return core.CreateSyncJobResult{
		Job:     existing,
		Created: false,
	}, nil
}

func (s *SyncJobStore) Get(ctx context.Context, id string) (core.SyncJob, error) {
	return s.GetSyncJob(ctx, id)
}

func (s *SyncJobStore) GetSyncJob(ctx context.Context, id string) (core.SyncJob, error) {
	if s == nil || s.db == nil {
		return core.SyncJob{}, fmt.Errorf("sqlstore: sync job store is not configured")
	}
	record := &syncJobRecord{}
	err := s.db.NewSelect().
		Model(record).
		Where("?TableAlias.id = ?", strings.TrimSpace(id)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return core.SyncJob{}, fmt.Errorf("%w: id %q", core.ErrSyncJobNotFound, id)
		}
		return core.SyncJob{}, err
	}
	return record.toDomain(), nil
}

func (s *SyncJobStore) Update(ctx context.Context, job core.SyncJob) (core.SyncJob, error) {
	if s == nil || s.db == nil {
		return core.SyncJob{}, fmt.Errorf("sqlstore: sync job store is not configured")
	}
	job.ID = strings.TrimSpace(job.ID)
	if job.ID == "" {
		return core.SyncJob{}, fmt.Errorf("sqlstore: job id is required")
	}
	job.UpdatedAt = time.Now().UTC()
	record := newSyncJobRecord(job, job.UpdatedAt)
	record.ID = job.ID
	record.CreatedAt = job.CreatedAt

	if _, err := s.db.NewUpdate().
		Model(record).
		Where("id = ?", record.ID).
		Exec(ctx); err != nil {
		return core.SyncJob{}, err
	}
	return record.toDomain(), nil
}

func (s *SyncJobStore) createSyncJobWithIdempotency(
	ctx context.Context,
	in core.CreateSyncJobStoreInput,
	job core.SyncJob,
) (core.SyncJob, error) {
	var created core.SyncJob
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now().UTC()
		job.ID = uuid.NewString()
		job.Attempts = 0
		job.CreatedAt = now
		job.UpdatedAt = now

		jobRecord := newSyncJobRecord(job, now)
		if _, insertErr := tx.NewInsert().Model(jobRecord).Exec(ctx); insertErr != nil {
			return insertErr
		}

		idempotencyRecord := &syncJobIdempotencyRecord{
			ID:             uuid.NewString(),
			ScopeType:      in.Scope.Type,
			ScopeID:        in.Scope.ID,
			ProviderID:     in.ProviderID,
			ConnectionID:   in.ConnectionID,
			Mode:           string(in.Mode),
			IdempotencyKey: in.IdempotencyKey,
			SyncJobID:      job.ID,
			RequestedBy:    in.RequestedBy,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if _, insertErr := tx.NewInsert().Model(idempotencyRecord).Exec(ctx); insertErr != nil {
			if isUniqueViolation(insertErr) {
				return errSyncJobIdempotencyReplay
			}
			return insertErr
		}
		created = jobRecord.toDomain()
		return nil
	})
	if err != nil {
		return core.SyncJob{}, err
	}
	return created, nil
}

func (s *SyncJobStore) findSyncJobIdempotency(
	ctx context.Context,
	scope core.ScopeRef,
	providerID string,
	mode core.SyncJobMode,
	idempotencyKey string,
) (*syncJobIdempotencyRecord, error) {
	record := &syncJobIdempotencyRecord{}
	err := s.db.NewSelect().
		Model(record).
		Where("?TableAlias.scope_type = ?", strings.TrimSpace(strings.ToLower(scope.Type))).
		Where("?TableAlias.scope_id = ?", strings.TrimSpace(scope.ID)).
		Where("?TableAlias.provider_id = ?", strings.TrimSpace(providerID)).
		Where("?TableAlias.mode = ?", strings.TrimSpace(strings.ToLower(string(mode)))).
		Where("?TableAlias.idempotency_key = ?", strings.TrimSpace(idempotencyKey)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(record.ID) == "" {
		return nil, nil
	}
	return record, nil
}

func normalizeCreateSyncJobStoreInput(
	in core.CreateSyncJobStoreInput,
) (core.CreateSyncJobStoreInput, error) {
	normalized := core.CreateSyncJobStoreInput{
		ProviderID: strings.TrimSpace(in.ProviderID),
		Scope: core.ScopeRef{
			Type: strings.TrimSpace(strings.ToLower(in.Scope.Type)),
			ID:   strings.TrimSpace(in.Scope.ID),
		},
		ConnectionID:   strings.TrimSpace(in.ConnectionID),
		Mode:           core.SyncJobMode(strings.TrimSpace(strings.ToLower(string(in.Mode)))),
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		RequestedBy:    strings.TrimSpace(in.RequestedBy),
		Metadata:       copyAnyMap(in.Metadata),
	}
	if normalized.ProviderID == "" {
		return core.CreateSyncJobStoreInput{}, fmt.Errorf("sqlstore: provider id is required")
	}
	if err := normalized.Scope.Validate(); err != nil {
		return core.CreateSyncJobStoreInput{}, fmt.Errorf("%w: %v", core.ErrInvalidSyncJobScope, err)
	}
	if normalized.ConnectionID == "" {
		return core.CreateSyncJobStoreInput{}, fmt.Errorf("sqlstore: connection id is required")
	}
	switch normalized.Mode {
	case core.SyncJobModeFull, core.SyncJobModeDelta:
	default:
		return core.CreateSyncJobStoreInput{}, fmt.Errorf("%w: %q", core.ErrInvalidSyncJobMode, in.Mode)
	}
	return normalized, nil
}
