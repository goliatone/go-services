package sqlstore

import (
	"time"

	"github.com/goliatone/go-services/core"
)

func newConnectionRecord(in core.CreateConnectionInput, now time.Time) *connectionRecord {
	return &connectionRecord{
		ProviderID:        in.ProviderID,
		ScopeType:         in.Scope.Type,
		ScopeID:           in.Scope.ID,
		ExternalAccountID: in.ExternalAccountID,
		Status:            string(in.Status),
		LastError:         "",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func (r *connectionRecord) toDomain() core.Connection {
	if r == nil {
		return core.Connection{}
	}
	connection := core.Connection{
		ID:                r.ID,
		ProviderID:        r.ProviderID,
		ScopeType:         r.ScopeType,
		ScopeID:           r.ScopeID,
		ExternalAccountID: r.ExternalAccountID,
		Status:            core.ConnectionStatus(r.Status),
		LastError:         r.LastError,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
	if r.InheritsFromConnectionID != nil {
		connection.InheritsFrom = *r.InheritsFromConnectionID
	}
	return connection
}

func newCredentialRecord(in core.SaveCredentialInput, version int, now time.Time) *credentialRecord {
	record := &credentialRecord{
		ConnectionID:      in.ConnectionID,
		Version:           version,
		EncryptedPayload:  append([]byte(nil), in.EncryptedPayload...),
		TokenType:         in.TokenType,
		RequestedScopes:   append([]string(nil), in.RequestedScopes...),
		GrantedScopes:     append([]string(nil), in.GrantedScopes...),
		Refreshable:       in.Refreshable,
		Status:            string(in.Status),
		GrantVersion:      version,
		EncryptionKeyID:   in.EncryptionKeyID,
		EncryptionVersion: in.EncryptionVersion,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if in.ExpiresAt != nil {
		expiresAt := *in.ExpiresAt
		record.ExpiresAt = &expiresAt
	}
	if in.RotatesAt != nil {
		rotatesAt := *in.RotatesAt
		record.RotatesAt = &rotatesAt
	}
	return record
}

func (r *credentialRecord) toDomain() core.Credential {
	if r == nil {
		return core.Credential{}
	}
	credential := core.Credential{
		ID:               r.ID,
		ConnectionID:     r.ConnectionID,
		Version:          r.Version,
		EncryptedPayload: append([]byte(nil), r.EncryptedPayload...),
		TokenType:        r.TokenType,
		RequestedScopes:  append([]string(nil), r.RequestedScopes...),
		GrantedScopes:    append([]string(nil), r.GrantedScopes...),
		Refreshable:      r.Refreshable,
		Status:           core.CredentialStatus(r.Status),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
	if r.ExpiresAt != nil {
		credential.ExpiresAt = *r.ExpiresAt
	}
	if r.RotatesAt != nil {
		credential.RotatesAt = *r.RotatesAt
	}
	return credential
}
