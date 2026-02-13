package sqlstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-services/core"
	"github.com/uptrace/bun"
)

type AppendServiceEventInput struct {
	ConnectionID string
	ProviderID   string
	ScopeType    string
	ScopeID      string
	EventType    string
	Status       string
	Error        string
	Metadata     map[string]any
}

type ServiceEventStore struct {
	repo repository.Repository[*serviceEventRecord]
}

func NewServiceEventStore(db *bun.DB) (*ServiceEventStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepository[*serviceEventRecord](db, eventHandlers())
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid service event repository wiring: %w", err)
		}
	}
	return &ServiceEventStore{repo: repo}, nil
}

func (s *ServiceEventStore) Append(ctx context.Context, in AppendServiceEventInput) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: service event store is not configured")
	}
	if strings.TrimSpace(in.ProviderID) == "" {
		return fmt.Errorf("sqlstore: provider id is required")
	}
	if strings.TrimSpace(in.ScopeType) == "" || strings.TrimSpace(in.ScopeID) == "" {
		return fmt.Errorf("sqlstore: scope type and scope id are required")
	}
	if strings.TrimSpace(in.EventType) == "" {
		return fmt.Errorf("sqlstore: event type is required")
	}
	if strings.TrimSpace(in.Status) == "" {
		return fmt.Errorf("sqlstore: event status is required")
	}

	var connectionID *string
	if trimmed := strings.TrimSpace(in.ConnectionID); trimmed != "" {
		connectionID = &trimmed
	}

	record := &serviceEventRecord{
		ConnectionID: connectionID,
		ProviderID:   strings.TrimSpace(in.ProviderID),
		ScopeType:    strings.TrimSpace(in.ScopeType),
		ScopeID:      strings.TrimSpace(in.ScopeID),
		EventType:    strings.TrimSpace(in.EventType),
		Status:       strings.TrimSpace(in.Status),
		Error:        strings.TrimSpace(in.Error),
		Metadata:     RedactMetadata(in.Metadata),
		CreatedAt:    time.Now().UTC(),
	}
	_, err := s.repo.Create(ctx, record)
	return err
}

type GrantStore struct {
	db             *bun.DB
	snapshotRepo   repository.Repository[*grantSnapshotRecord]
	eventRepo      repository.Repository[*grantEventRecord]
	connectionRepo repository.Repository[*connectionRecord]
}

func NewGrantStore(db *bun.DB) (*GrantStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	snapshotRepo := repository.NewRepository[*grantSnapshotRecord](db, grantSnapshotHandlers())
	if validator, ok := snapshotRepo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid grant snapshot repository wiring: %w", err)
		}
	}
	eventRepo := repository.NewRepository[*grantEventRecord](db, grantEventHandlers())
	if validator, ok := eventRepo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid grant event repository wiring: %w", err)
		}
	}
	connectionRepo := repository.NewRepository[*connectionRecord](db, connectionHandlers())
	if validator, ok := connectionRepo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid connection repository wiring: %w", err)
		}
	}
	return &GrantStore{
		db:             db,
		snapshotRepo:   snapshotRepo,
		eventRepo:      eventRepo,
		connectionRepo: connectionRepo,
	}, nil
}

func (s *GrantStore) SaveSnapshot(ctx context.Context, in core.SaveGrantSnapshotInput) error {
	if s == nil || s.snapshotRepo == nil || s.db == nil {
		return fmt.Errorf("sqlstore: grant store is not configured")
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		connection, err := s.loadConnectionTx(ctx, tx, in.ConnectionID)
		if err != nil {
			return err
		}
		return s.saveSnapshotTx(ctx, tx, connection, in)
	})
}

func (s *GrantStore) GetLatestSnapshot(ctx context.Context, connectionID string) (core.GrantSnapshot, bool, error) {
	if s == nil || s.snapshotRepo == nil {
		return core.GrantSnapshot{}, false, fmt.Errorf("sqlstore: grant store is not configured")
	}
	records, _, err := s.snapshotRepo.List(ctx,
		repository.SelectBy("connection_id", "=", strings.TrimSpace(connectionID)),
		repository.OrderBy("version DESC"),
		repository.OrderBy("captured_at DESC"),
		repository.SelectPaginate(1, 0),
	)
	if err != nil {
		return core.GrantSnapshot{}, false, err
	}
	if len(records) == 0 {
		return core.GrantSnapshot{}, false, nil
	}

	record := records[0]
	return core.GrantSnapshot{
		ConnectionID: record.ConnectionID,
		Version:      record.Version,
		Requested:    append([]string(nil), record.RequestedGrants...),
		Granted:      append([]string(nil), record.GrantedGrants...),
		CapturedAt:   record.CapturedAt,
		Metadata:     RedactMetadata(record.Metadata),
	}, true, nil
}

func (s *GrantStore) AppendEvent(ctx context.Context, in core.AppendGrantEventInput) error {
	if s == nil || s.eventRepo == nil || s.db == nil {
		return fmt.Errorf("sqlstore: grant store is not configured")
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		connection, err := s.loadConnectionTx(ctx, tx, in.ConnectionID)
		if err != nil {
			return err
		}
		return s.appendEventTx(ctx, tx, connection, in)
	})
}

func (s *GrantStore) SaveSnapshotAndEvent(
	ctx context.Context,
	snapshot core.SaveGrantSnapshotInput,
	event *core.AppendGrantEventInput,
) error {
	if s == nil || s.snapshotRepo == nil || s.eventRepo == nil || s.db == nil {
		return fmt.Errorf("sqlstore: grant store is not configured")
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		connection, err := s.loadConnectionTx(ctx, tx, snapshot.ConnectionID)
		if err != nil {
			return err
		}
		if err := s.saveSnapshotTx(ctx, tx, connection, snapshot); err != nil {
			return err
		}
		if event != nil {
			appendInput := *event
			appendInput.ConnectionID = snapshot.ConnectionID
			if err := s.appendEventTx(ctx, tx, connection, appendInput); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *GrantStore) saveSnapshotTx(
	ctx context.Context,
	tx bun.Tx,
	connection connectionRecord,
	in core.SaveGrantSnapshotInput,
) error {
	version := in.Version
	if version <= 0 {
		return fmt.Errorf("sqlstore: snapshot version must be greater than zero")
	}

	capturedAt := in.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	metadata := RedactMetadata(in.Metadata)
	metadata["snapshot_version"] = version
	metadata["captured_at"] = capturedAt.UTC().Format(time.RFC3339Nano)
	now := time.Now().UTC()
	record := &grantSnapshotRecord{
		ConnectionID:    connection.ID,
		ProviderID:      connection.ProviderID,
		ScopeType:       connection.ScopeType,
		ScopeID:         connection.ScopeID,
		Version:         version,
		RequestedGrants: append([]string(nil), in.Requested...),
		GrantedGrants:   append([]string(nil), in.Granted...),
		Metadata:        metadata,
		CapturedAt:      capturedAt.UTC(),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_, err := s.snapshotRepo.CreateTx(ctx, tx, record)
	return err
}

func (s *GrantStore) appendEventTx(
	ctx context.Context,
	tx bun.Tx,
	connection connectionRecord,
	in core.AppendGrantEventInput,
) error {
	eventType := strings.TrimSpace(in.EventType)
	if eventType == "" {
		return fmt.Errorf("sqlstore: grant event type is required")
	}
	occurredAt := in.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	record := &grantEventRecord{
		ConnectionID:    connection.ID,
		ProviderID:      connection.ProviderID,
		ScopeType:       connection.ScopeType,
		ScopeID:         connection.ScopeID,
		EventType:       eventType,
		RequestedGrants: []string{},
		GrantedGrants:   []string{},
		AddedGrants:     append([]string(nil), in.Added...),
		RemovedGrants:   append([]string(nil), in.Removed...),
		Metadata:        RedactMetadata(in.Metadata),
		CreatedAt:       occurredAt.UTC(),
	}
	_, err := s.eventRepo.CreateTx(ctx, tx, record)
	return err
}

func (s *GrantStore) loadConnectionTx(
	ctx context.Context,
	tx bun.Tx,
	connectionID string,
) (connectionRecord, error) {
	id := strings.TrimSpace(connectionID)
	if id == "" {
		return connectionRecord{}, fmt.Errorf("sqlstore: connection id is required")
	}
	record := connectionRecord{}
	if err := tx.NewSelect().
		Model(&record).
		Where("?TableAlias.id = ?", id).
		Limit(1).
		Scan(ctx); err != nil {
		return connectionRecord{}, err
	}
	return record, nil
}

var _ core.GrantStore = (*GrantStore)(nil)
var _ core.GrantStoreTransactional = (*GrantStore)(nil)
