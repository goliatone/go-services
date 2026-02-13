package sqlstore

import (
	"time"

	"github.com/uptrace/bun"
)

type connectionRecord struct {
	bun.BaseModel `bun:"table:service_connections,alias:sc"`

	ID                       string     `bun:"id,pk"`
	ProviderID               string     `bun:"provider_id,notnull"`
	ScopeType                string     `bun:"scope_type,notnull"`
	ScopeID                  string     `bun:"scope_id,notnull"`
	ExternalAccountID        string     `bun:"external_account_id,notnull"`
	Status                   string     `bun:"status,notnull"`
	InheritsFromConnectionID *string    `bun:"inherits_from_connection_id"`
	LastError                string     `bun:"last_error"`
	CreatedAt                time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt                time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt                *time.Time `bun:"deleted_at,soft_delete"`
}

type credentialRecord struct {
	bun.BaseModel `bun:"table:service_credentials,alias:scr"`

	ID                string     `bun:"id,pk"`
	ConnectionID      string     `bun:"connection_id,notnull"`
	Version           int        `bun:"version,notnull"`
	EncryptedPayload  []byte     `bun:"encrypted_payload,notnull"`
	TokenType         string     `bun:"token_type,notnull"`
	RequestedScopes   []string   `bun:"requested_scopes,type:jsonb,notnull"`
	GrantedScopes     []string   `bun:"granted_scopes,type:jsonb,notnull"`
	ExpiresAt         *time.Time `bun:"expires_at,nullzero"`
	RotatesAt         *time.Time `bun:"rotates_at,nullzero"`
	Refreshable       bool       `bun:"refreshable,notnull"`
	Status            string     `bun:"status,notnull"`
	GrantVersion      int        `bun:"grant_version,notnull"`
	EncryptionKeyID   string     `bun:"encryption_key_id,notnull"`
	EncryptionVersion int        `bun:"encryption_version,notnull"`
	RevocationReason  string     `bun:"revocation_reason,notnull"`
	CreatedAt         time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt         time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type serviceEventRecord struct {
	bun.BaseModel `bun:"table:service_events,alias:se"`

	ID           string         `bun:"id,pk"`
	ConnectionID *string        `bun:"connection_id"`
	ProviderID   string         `bun:"provider_id,notnull"`
	ScopeType    string         `bun:"scope_type,notnull"`
	ScopeID      string         `bun:"scope_id,notnull"`
	EventType    string         `bun:"event_type,notnull"`
	Status       string         `bun:"status,notnull"`
	Error        string         `bun:"error"`
	Metadata     map[string]any `bun:"metadata,type:jsonb,notnull"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

type grantEventRecord struct {
	bun.BaseModel `bun:"table:service_grant_events,alias:sge"`

	ID              string         `bun:"id,pk"`
	ConnectionID    string         `bun:"connection_id,notnull"`
	ProviderID      string         `bun:"provider_id,notnull"`
	ScopeType       string         `bun:"scope_type,notnull"`
	ScopeID         string         `bun:"scope_id,notnull"`
	EventType       string         `bun:"event_type,notnull"`
	RequestedGrants []string       `bun:"requested_grants,type:jsonb,notnull"`
	GrantedGrants   []string       `bun:"granted_grants,type:jsonb,notnull"`
	AddedGrants     []string       `bun:"added_grants,type:jsonb,notnull"`
	RemovedGrants   []string       `bun:"removed_grants,type:jsonb,notnull"`
	Metadata        map[string]any `bun:"metadata,type:jsonb,notnull"`
	CreatedAt       time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
