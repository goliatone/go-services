package sqlstore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-services/core"
	"github.com/uptrace/bun"
)

const grantSnapshotEventType = "snapshot"

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
	repo           repository.Repository[*grantEventRecord]
	connectionRepo repository.Repository[*connectionRecord]
}

func NewGrantStore(db *bun.DB) (*GrantStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	grantRepo := repository.NewRepository[*grantEventRecord](db, grantEventHandlers())
	if validator, ok := grantRepo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid grant repository wiring: %w", err)
		}
	}
	connectionRepo := repository.NewRepository[*connectionRecord](db, connectionHandlers())
	if validator, ok := connectionRepo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid connection repository wiring: %w", err)
		}
	}
	return &GrantStore{
		repo:           grantRepo,
		connectionRepo: connectionRepo,
	}, nil
}

func (s *GrantStore) SaveSnapshot(ctx context.Context, in core.SaveGrantSnapshotInput) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: grant store is not configured")
	}
	metadata := RedactMetadata(in.Metadata)
	metadata["snapshot_version"] = in.Version
	metadata["captured_at"] = in.CapturedAt.UTC().Format(time.RFC3339Nano)

	connection, err := s.connectionRepo.GetByID(ctx, strings.TrimSpace(in.ConnectionID))
	if err != nil {
		return err
	}

	record := &grantEventRecord{
		ConnectionID:    connection.ID,
		ProviderID:      connection.ProviderID,
		ScopeType:       connection.ScopeType,
		ScopeID:         connection.ScopeID,
		EventType:       grantSnapshotEventType,
		RequestedGrants: append([]string(nil), in.Requested...),
		GrantedGrants:   append([]string(nil), in.Granted...),
		AddedGrants:     []string{},
		RemovedGrants:   []string{},
		Metadata:        metadata,
		CreatedAt:       time.Now().UTC(),
	}
	_, err = s.repo.Create(ctx, record)
	return err
}

func (s *GrantStore) GetLatestSnapshot(ctx context.Context, connectionID string) (core.GrantSnapshot, error) {
	if s == nil || s.repo == nil {
		return core.GrantSnapshot{}, fmt.Errorf("sqlstore: grant store is not configured")
	}
	records, _, err := s.repo.List(ctx,
		repository.SelectBy("connection_id", "=", strings.TrimSpace(connectionID)),
		repository.SelectBy("event_type", "=", grantSnapshotEventType),
		repository.OrderBy("created_at DESC"),
		repository.SelectPaginate(1, 0),
	)
	if err != nil {
		return core.GrantSnapshot{}, err
	}
	if len(records) == 0 {
		return core.GrantSnapshot{}, fmt.Errorf("sqlstore: grant snapshot not found for connection %q", connectionID)
	}

	record := records[0]
	version := 0
	if rawVersion, ok := record.Metadata["snapshot_version"]; ok {
		version = toInt(rawVersion)
	}
	capturedAt := record.CreatedAt
	if rawCapturedAt, ok := record.Metadata["captured_at"]; ok {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, fmt.Sprint(rawCapturedAt)); parseErr == nil {
			capturedAt = parsed
		}
	}

	return core.GrantSnapshot{
		ConnectionID: record.ConnectionID,
		Version:      version,
		Requested:    append([]string(nil), record.RequestedGrants...),
		Granted:      append([]string(nil), record.GrantedGrants...),
		CapturedAt:   capturedAt,
		Metadata:     RedactMetadata(record.Metadata),
	}, nil
}

func (s *GrantStore) AppendEvent(ctx context.Context, in core.AppendGrantEventInput) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: grant store is not configured")
	}
	connection, err := s.connectionRepo.GetByID(ctx, strings.TrimSpace(in.ConnectionID))
	if err != nil {
		return err
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
		EventType:       strings.TrimSpace(in.EventType),
		RequestedGrants: []string{},
		GrantedGrants:   []string{},
		AddedGrants:     append([]string(nil), in.Added...),
		RemovedGrants:   append([]string(nil), in.Removed...),
		Metadata:        RedactMetadata(in.Metadata),
		CreatedAt:       occurredAt.UTC(),
	}
	_, err = s.repo.Create(ctx, record)
	return err
}

func toInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return 0
}

var _ core.GrantStore = (*GrantStore)(nil)
