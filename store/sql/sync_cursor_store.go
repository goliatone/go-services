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

	in.ConnectionID = strings.TrimSpace(in.ConnectionID)
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	in.ResourceType = strings.TrimSpace(in.ResourceType)
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	in.Cursor = strings.TrimSpace(in.Cursor)
	in.Status = strings.TrimSpace(in.Status)
	if in.ConnectionID == "" || in.ProviderID == "" {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: connection id and provider id are required")
	}
	if in.ResourceType == "" || in.ResourceID == "" {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: resource type and resource id are required")
	}
	if in.Cursor == "" {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: cursor is required")
	}
	if in.Status == "" {
		in.Status = "active"
	}
	now := time.Now().UTC()

	var out core.SyncCursor
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		record, err := findSyncCursorTx(ctx, tx, in.ConnectionID, in.ProviderID, in.ResourceType, in.ResourceID)
		if err != nil {
			return err
		}
		if record == nil {
			record = newSyncCursorRecord(in, now)
			record.ID = uuid.NewString()
			if _, insertErr := tx.NewInsert().Model(record).Exec(ctx); insertErr != nil {
				if isUniqueViolation(insertErr) {
					record, err = findSyncCursorTx(ctx, tx, in.ConnectionID, in.ProviderID, in.ResourceType, in.ResourceID)
					if err != nil {
						return err
					}
					if record == nil {
						return insertErr
					}
				} else {
					return insertErr
				}
			}
			out = record.toDomain()
			return nil
		}

		record.Cursor = in.Cursor
		record.Status = in.Status
		record.Metadata = copyAnyMap(in.Metadata)
		record.UpdatedAt = now
		if in.LastSyncedAt != nil {
			value := *in.LastSyncedAt
			record.LastSyncedAt = &value
		} else {
			record.LastSyncedAt = nil
		}
		if _, updateErr := tx.NewUpdate().Model(record).Where("id = ?", record.ID).Exec(ctx); updateErr != nil {
			return updateErr
		}
		out = record.toDomain()
		return nil
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
	if upsertInput.Status == "" {
		upsertInput.Status = "active"
	}
	if upsertInput.ConnectionID == "" || upsertInput.ProviderID == "" {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: connection id and provider id are required")
	}
	if upsertInput.ResourceType == "" || upsertInput.ResourceID == "" {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: resource type and resource id are required")
	}
	if upsertInput.Cursor == "" {
		return core.SyncCursor{}, fmt.Errorf("sqlstore: cursor is required")
	}

	var out core.SyncCursor
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		record, err := findSyncCursorTx(
			ctx,
			tx,
			upsertInput.ConnectionID,
			upsertInput.ProviderID,
			upsertInput.ResourceType,
			upsertInput.ResourceID,
		)
		if err != nil {
			return err
		}
		if record == nil {
			if expectedCursor != "" {
				return core.ErrSyncCursorConflict
			}
			record = newSyncCursorRecord(upsertInput, time.Now().UTC())
			record.ID = uuid.NewString()
			if _, insertErr := tx.NewInsert().Model(record).Exec(ctx); insertErr != nil {
				return insertErr
			}
			out = record.toDomain()
			return nil
		}

		if expectedCursor != "" && !strings.EqualFold(record.Cursor, expectedCursor) {
			return core.ErrSyncCursorConflict
		}

		record.Cursor = upsertInput.Cursor
		record.Status = upsertInput.Status
		record.Metadata = copyAnyMap(upsertInput.Metadata)
		record.UpdatedAt = time.Now().UTC()
		if upsertInput.LastSyncedAt != nil {
			value := *upsertInput.LastSyncedAt
			record.LastSyncedAt = &value
		}
		if _, updateErr := tx.NewUpdate().Model(record).Where("id = ?", record.ID).Exec(ctx); updateErr != nil {
			return updateErr
		}
		out = record.toDomain()
		return nil
	})
	if err != nil {
		return core.SyncCursor{}, err
	}
	return out, nil
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
