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

type CredentialStore struct {
	db   *bun.DB
	repo repository.Repository[*credentialRecord]
}

func (s *CredentialStore) SaveNewVersion(ctx context.Context, in core.SaveCredentialInput) (core.Credential, error) {
	if s == nil || s.repo == nil || s.db == nil {
		return core.Credential{}, fmt.Errorf("sqlstore: credential store is not configured")
	}
	trimmedConnectionID := strings.TrimSpace(in.ConnectionID)
	if trimmedConnectionID == "" {
		return core.Credential{}, fmt.Errorf("sqlstore: connection id is required")
	}

	status := in.Status
	if strings.TrimSpace(string(status)) == "" {
		status = core.CredentialStatusActive
	}
	in.ConnectionID = trimmedConnectionID
	in.Status = status
	now := time.Now().UTC()

	var created core.Credential
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		nextVersion, versionErr := s.nextVersion(ctx, tx, trimmedConnectionID)
		if versionErr != nil {
			return versionErr
		}

		if status == core.CredentialStatusActive {
			revokeReason := "rotated"
			_, updateErr := tx.NewUpdate().
				Model((*credentialRecord)(nil)).
				Set("status = ?", string(core.CredentialStatusRevoked)).
				Set("revocation_reason = ?", revokeReason).
				Set("updated_at = ?", now).
				Where("connection_id = ?", trimmedConnectionID).
				Where("status = ?", string(core.CredentialStatusActive)).
				Exec(ctx)
			if updateErr != nil {
				return updateErr
			}
		}

		record := newCredentialRecord(in, nextVersion, now)
		inserted, createErr := s.repo.CreateTx(ctx, tx, record)
		if createErr != nil {
			return createErr
		}
		created = inserted.toDomain()
		return nil
	})
	if err != nil {
		return core.Credential{}, err
	}

	return created, nil
}

func (s *CredentialStore) GetActiveByConnection(ctx context.Context, connectionID string) (core.Credential, error) {
	if s == nil || s.repo == nil {
		return core.Credential{}, fmt.Errorf("sqlstore: credential store is not configured")
	}
	records, _, err := s.repo.List(ctx,
		repository.SelectBy("connection_id", "=", strings.TrimSpace(connectionID)),
		repository.SelectBy("status", "=", string(core.CredentialStatusActive)),
		repository.OrderBy("version DESC"),
		repository.SelectPaginate(1, 0),
	)
	if err != nil {
		return core.Credential{}, err
	}
	if len(records) == 0 {
		return core.Credential{}, fmt.Errorf("sqlstore: active credential not found for connection %q", connectionID)
	}
	return records[0].toDomain(), nil
}

func (s *CredentialStore) RevokeActive(ctx context.Context, connectionID string, reason string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlstore: credential store is not configured")
	}
	trimmedConnectionID := strings.TrimSpace(connectionID)
	if trimmedConnectionID == "" {
		return fmt.Errorf("sqlstore: connection id is required")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "revoked"
	}

	_, err := s.db.NewUpdate().
		Model((*credentialRecord)(nil)).
		Set("status = ?", string(core.CredentialStatusRevoked)).
		Set("revocation_reason = ?", reason).
		Set("updated_at = ?", time.Now().UTC()).
		Where("connection_id = ?", trimmedConnectionID).
		Where("status = ?", string(core.CredentialStatusActive)).
		Exec(ctx)
	return err
}

func (s *CredentialStore) nextVersion(ctx context.Context, tx bun.Tx, connectionID string) (int, error) {
	var maxVersion int
	if err := tx.NewSelect().
		Model((*credentialRecord)(nil)).
		ColumnExpr("COALESCE(MAX(version), 0)").
		Where("?TableAlias.connection_id = ?", connectionID).
		Scan(ctx, &maxVersion); err != nil {
		return 0, err
	}
	return maxVersion + 1, nil
}
