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
	payloadFormat := in.PayloadFormat
	if payloadFormat == "" {
		payloadFormat = core.CredentialPayloadFormatLegacyToken
	}
	payloadVersion := in.PayloadVersion
	if payloadVersion <= 0 {
		payloadVersion = core.CredentialPayloadVersionV1
	}
	record := &credentialRecord{
		ConnectionID:      in.ConnectionID,
		Version:           version,
		EncryptedPayload:  append([]byte(nil), in.EncryptedPayload...),
		PayloadFormat:     payloadFormat,
		PayloadVersion:    payloadVersion,
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
		PayloadFormat:    r.PayloadFormat,
		PayloadVersion:   r.PayloadVersion,
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

func newSubscriptionRecord(in core.UpsertSubscriptionInput, now time.Time) *subscriptionRecord {
	record := &subscriptionRecord{
		ConnectionID:         in.ConnectionID,
		ProviderID:           in.ProviderID,
		ResourceType:         in.ResourceType,
		ResourceID:           in.ResourceID,
		ChannelID:            in.ChannelID,
		RemoteSubscriptionID: in.RemoteSubscriptionID,
		CallbackURL:          in.CallbackURL,
		VerificationTokenRef: in.VerificationTokenRef,
		Status:               string(in.Status),
		Metadata:             copyAnyMap(in.Metadata),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if in.ExpiresAt != nil {
		value := *in.ExpiresAt
		record.ExpiresAt = &value
	}
	return record
}

func (r *subscriptionRecord) toDomain() core.Subscription {
	if r == nil {
		return core.Subscription{}
	}
	subscription := core.Subscription{
		ID:                   r.ID,
		ConnectionID:         r.ConnectionID,
		ProviderID:           r.ProviderID,
		ResourceType:         r.ResourceType,
		ResourceID:           r.ResourceID,
		ChannelID:            r.ChannelID,
		RemoteSubscriptionID: r.RemoteSubscriptionID,
		CallbackURL:          r.CallbackURL,
		VerificationTokenRef: r.VerificationTokenRef,
		Status:               core.SubscriptionStatus(r.Status),
		Metadata:             copyAnyMap(r.Metadata),
		CreatedAt:            r.CreatedAt,
		UpdatedAt:            r.UpdatedAt,
	}
	if r.ExpiresAt != nil {
		subscription.ExpiresAt = *r.ExpiresAt
	}
	return subscription
}

func newSyncCursorRecord(in core.UpsertSyncCursorInput, now time.Time) *syncCursorRecord {
	record := &syncCursorRecord{
		ConnectionID: in.ConnectionID,
		ProviderID:   in.ProviderID,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		Cursor:       in.Cursor,
		Status:       in.Status,
		Metadata:     copyAnyMap(in.Metadata),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if in.LastSyncedAt != nil {
		value := *in.LastSyncedAt
		record.LastSyncedAt = &value
	}
	return record
}

func (r *syncCursorRecord) toDomain() core.SyncCursor {
	if r == nil {
		return core.SyncCursor{}
	}
	cursor := core.SyncCursor{
		ID:           r.ID,
		ConnectionID: r.ConnectionID,
		ProviderID:   r.ProviderID,
		ResourceType: r.ResourceType,
		ResourceID:   r.ResourceID,
		Cursor:       r.Cursor,
		Status:       r.Status,
		Metadata:     copyAnyMap(r.Metadata),
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
	if r.LastSyncedAt != nil {
		cursor.LastSyncedAt = *r.LastSyncedAt
	}
	return cursor
}

func newInstallationRecord(in core.UpsertInstallationInput, now time.Time) *installationRecord {
	record := &installationRecord{
		ProviderID:  in.ProviderID,
		ScopeType:   in.Scope.Type,
		ScopeID:     in.Scope.ID,
		InstallType: in.InstallType,
		Status:      string(in.Status),
		Metadata:    copyAnyMap(in.Metadata),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if in.GrantedAt != nil {
		value := *in.GrantedAt
		record.GrantedAt = &value
	}
	if in.RevokedAt != nil {
		value := *in.RevokedAt
		record.RevokedAt = &value
	}
	return record
}

func (r *installationRecord) toDomain() core.Installation {
	if r == nil {
		return core.Installation{}
	}
	installation := core.Installation{
		ID:          r.ID,
		ProviderID:  r.ProviderID,
		ScopeType:   r.ScopeType,
		ScopeID:     r.ScopeID,
		InstallType: r.InstallType,
		Status:      core.InstallationStatus(r.Status),
		Metadata:    copyAnyMap(r.Metadata),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if r.GrantedAt != nil {
		value := *r.GrantedAt
		installation.GrantedAt = &value
	}
	if r.RevokedAt != nil {
		value := *r.RevokedAt
		installation.RevokedAt = &value
	}
	return installation
}

func (r *syncJobRecord) toDomain() core.SyncJob {
	if r == nil {
		return core.SyncJob{}
	}
	job := core.SyncJob{
		ID:           r.ID,
		ConnectionID: r.ConnectionID,
		ProviderID:   r.ProviderID,
		Mode:         core.SyncJobMode(r.Mode),
		Checkpoint:   r.Checkpoint,
		Status:       core.SyncJobStatus(r.Status),
		Attempts:     r.Attempts,
		Metadata:     copyAnyMap(r.Metadata),
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
	if r.NextAttemptAt != nil {
		nextAttempt := *r.NextAttemptAt
		job.NextAttemptAt = &nextAttempt
	}
	return job
}

func newSyncJobRecord(job core.SyncJob, now time.Time) *syncJobRecord {
	record := &syncJobRecord{
		ID:           job.ID,
		ConnectionID: job.ConnectionID,
		ProviderID:   job.ProviderID,
		Mode:         string(job.Mode),
		Checkpoint:   job.Checkpoint,
		Status:       string(job.Status),
		Attempts:     job.Attempts,
		Metadata:     copyAnyMap(job.Metadata),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if job.NextAttemptAt != nil {
		value := *job.NextAttemptAt
		record.NextAttemptAt = &value
	}
	return record
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
