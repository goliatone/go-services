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

type SyncCursorStore struct {
	db   *bun.DB
	repo repository.Repository[*syncCursorRecord]
}

func NewSyncCursorStore(db *bun.DB) (*SyncCursorStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepository[*syncCursorRecord](db, syncCursorHandlers())
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid sync cursor repository wiring: %w", err)
		}
	}
	return &SyncCursorStore{
		db:   db,
		repo: repo,
	}, nil
}

func (s *SyncCursorStore) Get(
	ctx context.Context,
	connectionID string,
	resourceType string,
	resourceID string,
) (core.SyncCursor, error) {
	if s == nil || s.db == nil {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: sync cursor store is not configured")
	}
	connectionID = strings.TrimSpace(connectionID)
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if connectionID == "" || resourceType == "" || resourceID == "" {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: connection id, resource type, and resource id are required")
	}

	record := &syncCursorRecord{}
	err := s.db.NewSelect().
		Model(record).
		Where("?TableAlias.connection_id = ?", connectionID).
		Where("?TableAlias.resource_type = ?", resourceType).
		Where("?TableAlias.resource_id = ?", resourceID).
		OrderExpr("?TableAlias.updated_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return core.SyncCursor{}, fmt.Errorf("sqlstore: sync cursor not found")
		}
		return core.SyncCursor{}, err
	}
	return record.toDomain(), nil
}

func (s *SyncCursorStore) Upsert(ctx context.Context, in core.UpsertSyncCursorInput) (core.SyncCursor, error) {
	if s == nil || s.db == nil {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: sync cursor store is not configured")
	}

	in = normalizeSyncCursorInput(in)
	if err := validateSyncCursorInput(in); err != nil {
		return core.SyncCursor{}, err
	}
	now := time.Now().UTC()

	var out core.SyncCursor
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		resolved, err := upsertSyncCursorTx(ctx, tx, in, now)
		out = resolved
		return err
	})
	if err != nil {
		return core.SyncCursor{}, err
	}
	return out, nil
}

func (s *SyncCursorStore) Advance(ctx context.Context, in core.AdvanceSyncCursorInput) (core.SyncCursor, error) {
	if s == nil || s.db == nil {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: sync cursor store is not configured")
	}
	upsertInput := core.UpsertSyncCursorInput{
		ConnectionID: strings.TrimSpace(in.ConnectionID),
		ProviderID:   strings.TrimSpace(in.ProviderID),
		ResourceType: strings.TrimSpace(in.ResourceType),
		ResourceID:   strings.TrimSpace(in.ResourceID),
		Cursor:       strings.TrimSpace(in.Cursor),
		LastSyncedAt: in.LastSyncedAt,
		Status:       strings.TrimSpace(in.Status),
		Metadata:     copyAnyMap(in.Metadata),
	}
	expectedCursor := strings.TrimSpace(in.ExpectedCursor)
	upsertInput = normalizeSyncCursorInput(upsertInput)
	if err := validateSyncCursorInput(upsertInput); err != nil {
		return core.SyncCursor{}, err
	}

	var out core.SyncCursor
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		resolved, err := advanceSyncCursorTx(ctx, tx, upsertInput, expectedCursor, time.Now().UTC())
		out = resolved
		return err
	})
	if err != nil {
		return core.SyncCursor{}, err
	}
	return out, nil
}

func normalizeSyncCursorInput(in core.UpsertSyncCursorInput) core.UpsertSyncCursorInput {
	in.ConnectionID = strings.TrimSpace(in.ConnectionID)
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	in.ResourceType = strings.TrimSpace(in.ResourceType)
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	in.Cursor = strings.TrimSpace(in.Cursor)
	in.Status = strings.TrimSpace(in.Status)
	if in.Status == "" {
		in.Status = "active"
	}
	return in
}

func validateSyncCursorInput(in core.UpsertSyncCursorInput) error {
	if in.ConnectionID == "" || in.ProviderID == "" {
		return fmt.Errorf("sqlstore: connection id and provider id are required")
	}
	if in.ResourceType == "" || in.ResourceID == "" {
		return fmt.Errorf("sqlstore: resource type and resource id are required")
	}
	if in.Cursor == "" {
		return fmt.Errorf("sqlstore: cursor is required")
	}
	return nil
}

func upsertSyncCursorTx(
	ctx context.Context,
	tx bun.Tx,
	in core.UpsertSyncCursorInput,
	now time.Time,
) (core.SyncCursor, error) {
	record, err := findSyncCursorTx(ctx, tx, in.ConnectionID, in.ProviderID, in.ResourceType, in.ResourceID)
	if err != nil {
		return core.SyncCursor{}, err
	}
	if record == nil {
		record, err = insertSyncCursorTx(ctx, tx, in, now)
		if err != nil {
			return core.SyncCursor{}, err
		}
		return record.toDomain(), nil
	}
	updateSyncCursorRecord(record, in, now, true)
	_, err = tx.NewUpdate().Model(record).Where("id = ?", record.ID).Exec(ctx)
	return record.toDomain(), err
}

func insertSyncCursorTx(
	ctx context.Context,
	tx bun.Tx,
	in core.UpsertSyncCursorInput,
	now time.Time,
) (*syncCursorRecord, error) {
	record := newSyncCursorRecord(in, now)
	record.ID = uuid.NewString()
	if _, err := tx.NewInsert().Model(record).Exec(ctx); err == nil {
		return record, nil
	} else if !isUniqueViolation(err) {
		return nil, err
	} else {
		existing, lookupErr := findSyncCursorTx(ctx, tx, in.ConnectionID, in.ProviderID, in.ResourceType, in.ResourceID)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if existing == nil {
			return nil, err
		}
		return existing, nil
	}
}

func advanceSyncCursorTx(
	ctx context.Context,
	tx bun.Tx,
	in core.UpsertSyncCursorInput,
	expectedCursor string,
	now time.Time,
) (core.SyncCursor, error) {
	record, err := findSyncCursorTx(ctx, tx, in.ConnectionID, in.ProviderID, in.ResourceType, in.ResourceID)
	if err != nil {
		return core.SyncCursor{}, err
	}
	if record == nil {
		if expectedCursor != "" {
			return core.SyncCursor{}, core.ErrSyncCursorConflict
		}
		record = newSyncCursorRecord(in, now)
		record.ID = uuid.NewString()
		_, err = tx.NewInsert().Model(record).Exec(ctx)
		return record.toDomain(), err
	}
	if expectedCursor != "" && !strings.EqualFold(record.Cursor, expectedCursor) {
		return core.SyncCursor{}, core.ErrSyncCursorConflict
	}
	updateSyncCursorRecord(record, in, now, false)
	_, err = tx.NewUpdate().Model(record).Where("id = ?", record.ID).Exec(ctx)
	return record.toDomain(), err
}

func updateSyncCursorRecord(
	record *syncCursorRecord,
	in core.UpsertSyncCursorInput,
	now time.Time,
	clearMissingSyncTime bool,
) {
	record.Cursor = in.Cursor
	record.Status = in.Status
	record.Metadata = copyAnyMap(in.Metadata)
	record.UpdatedAt = now
	if in.LastSyncedAt != nil {
		value := *in.LastSyncedAt
		record.LastSyncedAt = &value
	} else if clearMissingSyncTime {
		record.LastSyncedAt = nil
	}
}

func findSyncCursorTx(
	ctx context.Context,
	tx bun.Tx,
	connectionID string,
	providerID string,
	resourceType string,
	resourceID string,
) (*syncCursorRecord, error) {
	record := &syncCursorRecord{}
	err := tx.NewSelect().
		Model(record).
		Where("?TableAlias.connection_id = ?", strings.TrimSpace(connectionID)).
		Where("?TableAlias.provider_id = ?", strings.TrimSpace(providerID)).
		Where("?TableAlias.resource_type = ?", strings.TrimSpace(resourceType)).
		Where("?TableAlias.resource_id = ?", strings.TrimSpace(resourceID)).
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
