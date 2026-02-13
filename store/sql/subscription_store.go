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

type SubscriptionStore struct {
	db   *bun.DB
	repo repository.Repository[*subscriptionRecord]
}

func NewSubscriptionStore(db *bun.DB) (*SubscriptionStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlstore: bun db is required")
	}
	repo := repository.NewRepository[*subscriptionRecord](db, subscriptionHandlers())
	if validator, ok := repo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("sqlstore: invalid subscription repository wiring: %w", err)
		}
	}
	return &SubscriptionStore{
		db:   db,
		repo: repo,
	}, nil
}

func (s *SubscriptionStore) Upsert(ctx context.Context, in core.UpsertSubscriptionInput) (core.Subscription, error) {
	if s == nil || s.db == nil || s.repo == nil {
		return core.Subscription{}, fmt.Errorf("sqlstore: subscription store is not configured")
	}
	in.ConnectionID = strings.TrimSpace(in.ConnectionID)
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	in.ResourceType = strings.TrimSpace(in.ResourceType)
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	in.ChannelID = strings.TrimSpace(in.ChannelID)
	in.CallbackURL = strings.TrimSpace(in.CallbackURL)
	in.RemoteSubscriptionID = strings.TrimSpace(in.RemoteSubscriptionID)
	in.VerificationTokenRef = strings.TrimSpace(in.VerificationTokenRef)
	if in.ConnectionID == "" || in.ProviderID == "" {
		return core.Subscription{}, fmt.Errorf("sqlstore: connection id and provider id are required")
	}
	if in.ResourceType == "" || in.ResourceID == "" {
		return core.Subscription{}, fmt.Errorf("sqlstore: resource type and resource id are required")
	}
	if in.ChannelID == "" {
		return core.Subscription{}, fmt.Errorf("sqlstore: channel id is required")
	}
	if in.CallbackURL == "" {
		return core.Subscription{}, fmt.Errorf("sqlstore: callback url is required")
	}
	if strings.TrimSpace(string(in.Status)) == "" {
		in.Status = core.SubscriptionStatusActive
	}
	now := time.Now().UTC()

	var out core.Subscription
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		existing, err := s.findByProviderChannelTx(ctx, tx, in.ProviderID, in.ChannelID)
		if err != nil {
			return err
		}
		if existing == nil {
			record := newSubscriptionRecord(in, now)
			record.ID = uuid.NewString()
			if _, createErr := tx.NewInsert().Model(record).Exec(ctx); createErr != nil {
				return createErr
			}
			out = record.toDomain()
			return nil
		}

		existing.ConnectionID = in.ConnectionID
		existing.ProviderID = in.ProviderID
		existing.ResourceType = in.ResourceType
		existing.ResourceID = in.ResourceID
		existing.ChannelID = in.ChannelID
		existing.RemoteSubscriptionID = in.RemoteSubscriptionID
		existing.CallbackURL = in.CallbackURL
		existing.VerificationTokenRef = in.VerificationTokenRef
		existing.Status = string(in.Status)
		existing.Metadata = copyAnyMap(in.Metadata)
		existing.UpdatedAt = now
		existing.DeletedAt = nil
		if in.ExpiresAt == nil {
			existing.ExpiresAt = nil
		} else {
			value := *in.ExpiresAt
			existing.ExpiresAt = &value
		}

		if _, updateErr := tx.NewUpdate().
			Model(existing).
			Where("id = ?", existing.ID).
			Exec(ctx); updateErr != nil {
			return updateErr
		}
		out = existing.toDomain()
		return nil
	})
	if err != nil {
		return core.Subscription{}, err
	}

	return out, nil
}

func (s *SubscriptionStore) Get(ctx context.Context, id string) (core.Subscription, error) {
	if s == nil || s.repo == nil {
		return core.Subscription{}, fmt.Errorf("sqlstore: subscription store is not configured")
	}
	record, err := s.repo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return core.Subscription{}, err
	}
	return record.toDomain(), nil
}

func (s *SubscriptionStore) GetByChannelID(ctx context.Context, providerID, channelID string) (core.Subscription, error) {
	if s == nil || s.repo == nil {
		return core.Subscription{}, fmt.Errorf("sqlstore: subscription store is not configured")
	}
	records, _, err := s.repo.List(ctx,
		repository.SelectBy("provider_id", "=", strings.TrimSpace(providerID)),
		repository.SelectBy("channel_id", "=", strings.TrimSpace(channelID)),
		repository.SelectRawProcessor(func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("?TableAlias.deleted_at IS NULL")
		}),
		repository.OrderBy("updated_at DESC"),
		repository.SelectPaginate(1, 0),
	)
	if err != nil {
		return core.Subscription{}, err
	}
	if len(records) == 0 {
		return core.Subscription{}, fmt.Errorf(
			"sqlstore: subscription not found for provider %q channel %q",
			providerID,
			channelID,
		)
	}
	return records[0].toDomain(), nil
}

func (s *SubscriptionStore) ListExpiring(ctx context.Context, before time.Time) ([]core.Subscription, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("sqlstore: subscription store is not configured")
	}
	records, _, err := s.repo.List(ctx,
		repository.SelectBy("status", "=", string(core.SubscriptionStatusActive)),
		repository.SelectRawProcessor(func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.
				Where("?TableAlias.deleted_at IS NULL").
				Where("?TableAlias.expires_at IS NOT NULL").
				Where("?TableAlias.expires_at <= ?", before.UTC())
		}),
		repository.OrderBy("expires_at ASC"),
	)
	if err != nil {
		return nil, err
	}
	out := make([]core.Subscription, 0, len(records))
	for _, record := range records {
		out = append(out, record.toDomain())
	}
	return out, nil
}

func (s *SubscriptionStore) UpdateState(ctx context.Context, id string, status string, reason string) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("sqlstore: subscription store is not configured")
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return fmt.Errorf("sqlstore: subscription id is required")
	}
	record, err := s.repo.GetByID(ctx, trimmedID)
	if err != nil {
		return err
	}
	record.Status = strings.TrimSpace(status)
	record.UpdatedAt = time.Now().UTC()
	record.Metadata = copyAnyMap(record.Metadata)
	if strings.TrimSpace(reason) != "" {
		record.Metadata["status_reason"] = strings.TrimSpace(reason)
		record.Metadata["status_reason_at"] = record.UpdatedAt.Format(time.RFC3339Nano)
	}
	_, err = s.repo.Update(ctx, record, repository.UpdateByID(trimmedID))
	return err
}

func (s *SubscriptionStore) findByProviderChannelTx(
	ctx context.Context,
	tx bun.Tx,
	providerID string,
	channelID string,
) (*subscriptionRecord, error) {
	record := &subscriptionRecord{}
	err := tx.NewSelect().
		Model(record).
		Where("?TableAlias.provider_id = ?", strings.TrimSpace(providerID)).
		Where("?TableAlias.channel_id = ?", strings.TrimSpace(channelID)).
		Where("?TableAlias.deleted_at IS NULL").
		OrderExpr("?TableAlias.updated_at DESC").
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
