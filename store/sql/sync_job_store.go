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

type SyncJobStore struct {
	db   *bun.DB
	repo repository.Repository[*syncJobRecord]
}

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

func (s *SyncJobStore) Get(ctx context.Context, id string) (core.SyncJob, error) {
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
			return core.SyncJob{}, fmt.Errorf("sqlstore: sync job not found for id %q", id)
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
