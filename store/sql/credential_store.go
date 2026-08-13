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
	prepared, err := prepareCredentialInput(in)
	if err != nil {
		return core.Credential{}, err
	}
	return runInTxResult(ctx, s.db, func(ctx context.Context, tx bun.Tx) (core.Credential, error) {
		return s.saveCredentialVersionTx(ctx, tx, prepared, time.Now().UTC())
	})
}

func prepareCredentialInput(in core.SaveCredentialInput) (core.SaveCredentialInput, error) {
	in = normalizeCredentialInput(in)
	return in, validateCredentialInput(in)
}

func runInTxResult[T any](
	ctx context.Context,
	db *bun.DB,
	operation func(context.Context, bun.Tx) (T, error),
) (T, error) {
	var result T
	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		resolved, operationErr := operation(ctx, tx)
		result = resolved
		return operationErr
	})
	return result, err
}

func normalizeCredentialInput(in core.SaveCredentialInput) core.SaveCredentialInput {
	in.ConnectionID = strings.TrimSpace(in.ConnectionID)
	if strings.TrimSpace(string(in.Status)) == "" {
		in.Status = core.CredentialStatusActive
	}
	in.EncryptionKeyID = strings.TrimSpace(in.EncryptionKeyID)
	in.PayloadFormat = strings.TrimSpace(in.PayloadFormat)
	if in.PayloadFormat == "" {
		in.PayloadFormat = core.CredentialPayloadFormatLegacyToken
	}
	if in.PayloadVersion <= 0 {
		in.PayloadVersion = core.CredentialPayloadVersionV1
	}
	return in
}

func validateCredentialInput(in core.SaveCredentialInput) error {
	if in.ConnectionID == "" {
		return fmt.Errorf("sqlstore: connection id is required")
	}
	if len(in.EncryptedPayload) == 0 {
		return fmt.Errorf("sqlstore: encrypted payload is required")
	}
	if in.EncryptionKeyID == "" {
		return fmt.Errorf("sqlstore: encryption key id is required")
	}
	if in.EncryptionVersion <= 0 {
		return fmt.Errorf("sqlstore: encryption version must be greater than zero")
	}
	return nil
}

func (s *CredentialStore) saveCredentialVersionTx(
	ctx context.Context,
	tx bun.Tx,
	in core.SaveCredentialInput,
	now time.Time,
) (core.Credential, error) {
	nextVersion, err := s.nextVersion(ctx, tx, in.ConnectionID)
	if err != nil {
		return core.Credential{}, err
	}
	if in.Status == core.CredentialStatusActive {
		_, err = tx.NewUpdate().
			Model((*credentialRecord)(nil)).
			Set("status = ?", string(core.CredentialStatusRevoked)).
			Set("revocation_reason = ?", "rotated").
			Set("updated_at = ?", now).
			Where("connection_id = ?", in.ConnectionID).
			Where("status = ?", string(core.CredentialStatusActive)).
			Exec(ctx)
		if err != nil {
			return core.Credential{}, err
		}
	}
	record := newCredentialRecord(in, nextVersion, now)
	inserted, err := s.repo.CreateTx(ctx, tx, record)
	if err != nil {
		return core.Credential{}, err
	}
	return inserted.toDomain(), nil
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
