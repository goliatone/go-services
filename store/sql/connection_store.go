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

type ConnectionStore struct {
	db   *bun.DB
	repo repository.Repository[*connectionRecord]
}

func (s *ConnectionStore) Create(ctx context.Context, in core.CreateConnectionInput) (core.Connection, error) {
	if s == nil || s.repo == nil {
		return core.Connection{}, fmt.Errorf("sqlstore: connection store is not configured")
	}
	if err := in.Scope.Validate(); err != nil {
		return core.Connection{}, err
	}
	if strings.TrimSpace(in.ProviderID) == "" {
		return core.Connection{}, fmt.Errorf("sqlstore: provider id is required")
	}
	if strings.TrimSpace(in.ExternalAccountID) == "" {
		return core.Connection{}, fmt.Errorf("sqlstore: external account id is required")
	}

	status := in.Status
	if strings.TrimSpace(string(status)) == "" {
		status = core.ConnectionStatusActive
	}

	record := newConnectionRecord(core.CreateConnectionInput{
		ProviderID:        in.ProviderID,
		Scope:             in.Scope,
		ExternalAccountID: in.ExternalAccountID,
		Status:            status,
	}, time.Now().UTC())

	created, err := s.repo.Create(ctx, record)
	if err != nil {
		return core.Connection{}, err
	}
	return created.toDomain(), nil
}

func (s *ConnectionStore) Get(ctx context.Context, id string) (core.Connection, error) {
	if s == nil || s.repo == nil {
		return core.Connection{}, fmt.Errorf("sqlstore: connection store is not configured")
	}
	record, err := s.repo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return core.Connection{}, err
	}
	return record.toDomain(), nil
}

func (s *ConnectionStore) FindByScope(ctx context.Context, providerID string, scope core.ScopeRef) ([]core.Connection, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("sqlstore: connection store is not configured")
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	records, _, err := s.repo.List(ctx,
		repository.SelectBy("provider_id", "=", strings.TrimSpace(providerID)),
		repository.SelectBy("scope_type", "=", strings.TrimSpace(scope.Type)),
		repository.SelectBy("scope_id", "=", strings.TrimSpace(scope.ID)),
		repository.SelectRawProcessor(func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("?TableAlias.deleted_at IS NULL")
		}),
		repository.OrderBy("created_at ASC"),
	)
	if err != nil {
		return nil, err
	}

	out := make([]core.Connection, 0, len(records))
	for _, record := range records {
		out = append(out, record.toDomain())
	}
	return out, nil
}

func (s *ConnectionStore) UpdateStatus(ctx context.Context, id string, status string, reason string) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: connection store is not configured")
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return fmt.Errorf("sqlstore: connection id is required")
	}
	current, err := s.repo.GetByID(ctx, trimmedID)
	if err != nil {
		return err
	}
	current.Status = strings.TrimSpace(status)
	current.LastError = strings.TrimSpace(reason)
	current.UpdatedAt = time.Now().UTC()

	_, err = s.repo.Update(ctx, current, repository.UpdateByID(trimmedID))
	return err
}
