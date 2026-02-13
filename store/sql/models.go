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
	PayloadFormat     string     `bun:"payload_format,notnull"`
	PayloadVersion    int        `bun:"payload_version,notnull"`
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

type activityEntryRecord struct {
	bun.BaseModel `bun:"table:service_activity_entries,alias:sae"`

	ID             string         `bun:"id,pk"`
	ProviderID     string         `bun:"provider_id,notnull"`
	ScopeType      string         `bun:"scope_type,notnull"`
	ScopeID        string         `bun:"scope_id,notnull"`
	ConnectionID   *string        `bun:"connection_id"`
	InstallationID *string        `bun:"installation_id"`
	SubscriptionID *string        `bun:"subscription_id"`
	SyncJobID      *string        `bun:"sync_job_id"`
	Channel        string         `bun:"channel,notnull"`
	Action         string         `bun:"action,notnull"`
	ObjectType     string         `bun:"object_type,notnull"`
	ObjectID       string         `bun:"object_id,notnull"`
	Actor          string         `bun:"actor,notnull"`
	ActorType      string         `bun:"actor_type,notnull"`
	Status         string         `bun:"status,notnull"`
	Metadata       map[string]any `bun:"metadata,type:jsonb,notnull"`
	CreatedAt      time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
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

type grantSnapshotRecord struct {
	bun.BaseModel `bun:"table:service_grant_snapshots,alias:sgs"`

	ID              string         `bun:"id,pk"`
	ConnectionID    string         `bun:"connection_id,notnull"`
	ProviderID      string         `bun:"provider_id,notnull"`
	ScopeType       string         `bun:"scope_type,notnull"`
	ScopeID         string         `bun:"scope_id,notnull"`
	Version         int            `bun:"version,notnull"`
	RequestedGrants []string       `bun:"requested_grants,type:jsonb,notnull"`
	GrantedGrants   []string       `bun:"granted_grants,type:jsonb,notnull"`
	Metadata        map[string]any `bun:"metadata,type:jsonb,notnull"`
	CapturedAt      time.Time      `bun:"captured_at,nullzero,notnull"`
	CreatedAt       time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type subscriptionRecord struct {
	bun.BaseModel `bun:"table:service_subscriptions,alias:ss"`

	ID                   string         `bun:"id,pk"`
	ConnectionID         string         `bun:"connection_id,notnull"`
	ProviderID           string         `bun:"provider_id,notnull"`
	ResourceType         string         `bun:"resource_type,notnull"`
	ResourceID           string         `bun:"resource_id,notnull"`
	ChannelID            string         `bun:"channel_id,notnull"`
	RemoteSubscriptionID string         `bun:"remote_subscription_id"`
	CallbackURL          string         `bun:"callback_url,notnull"`
	VerificationTokenRef string         `bun:"verification_token_ref,notnull"`
	Status               string         `bun:"status,notnull"`
	ExpiresAt            *time.Time     `bun:"expires_at,nullzero"`
	Metadata             map[string]any `bun:"metadata,type:jsonb,notnull"`
	LastNotifiedAt       *time.Time     `bun:"last_notified_at,nullzero"`
	CreatedAt            time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt            time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt            *time.Time     `bun:"deleted_at,soft_delete"`
}

type webhookDeliveryRecord struct {
	bun.BaseModel `bun:"table:service_webhook_deliveries,alias:swd"`

	ID            string     `bun:"id,pk"`
	ProviderID    string     `bun:"provider_id,notnull"`
	DeliveryID    string     `bun:"delivery_id,notnull"`
	Status        string     `bun:"status,notnull"`
	Attempts      int        `bun:"attempts,notnull"`
	NextAttemptAt *time.Time `bun:"next_attempt_at,nullzero"`
	Payload       []byte     `bun:"payload"`
	CreatedAt     time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type syncCursorRecord struct {
	bun.BaseModel `bun:"table:service_sync_cursors,alias:ssc"`

	ID           string         `bun:"id,pk"`
	ConnectionID string         `bun:"connection_id,notnull"`
	ProviderID   string         `bun:"provider_id,notnull"`
	ResourceType string         `bun:"resource_type,notnull"`
	ResourceID   string         `bun:"resource_id,notnull"`
	Cursor       string         `bun:"cursor,notnull"`
	Status       string         `bun:"status,notnull"`
	LastSyncedAt *time.Time     `bun:"last_synced_at,nullzero"`
	Metadata     map[string]any `bun:"metadata,type:jsonb,notnull"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type syncJobRecord struct {
	bun.BaseModel `bun:"table:service_sync_jobs,alias:ssj"`

	ID            string         `bun:"id,pk"`
	ConnectionID  string         `bun:"connection_id,notnull"`
	ProviderID    string         `bun:"provider_id,notnull"`
	Mode          string         `bun:"mode,notnull"`
	Checkpoint    string         `bun:"checkpoint"`
	Status        string         `bun:"status,notnull"`
	Attempts      int            `bun:"attempts,notnull"`
	NextAttemptAt *time.Time     `bun:"next_attempt_at,nullzero"`
	Metadata      map[string]any `bun:"metadata,type:jsonb,notnull"`
	CreatedAt     time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type lifecycleOutboxRecord struct {
	bun.BaseModel `bun:"table:service_lifecycle_outbox,alias:slo"`

	ID           string         `bun:"id,pk"`
	EventID      string         `bun:"event_id,notnull"`
	EventName    string         `bun:"event_name,notnull"`
	ProviderID   string         `bun:"provider_id,notnull"`
	ScopeType    string         `bun:"scope_type,notnull"`
	ScopeID      string         `bun:"scope_id,notnull"`
	ConnectionID *string        `bun:"connection_id"`
	Payload      map[string]any `bun:"payload,type:jsonb,notnull"`
	Metadata     map[string]any `bun:"metadata,type:jsonb,notnull"`
	Status       string         `bun:"status,notnull"`
	Attempts     int            `bun:"attempts,notnull"`
	NextAttempt  *time.Time     `bun:"next_attempt_at,nullzero"`
	LastError    string         `bun:"last_error,notnull"`
	OccurredAt   time.Time      `bun:"occurred_at,notnull"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type notificationDispatchRecord struct {
	bun.BaseModel `bun:"table:service_notification_dispatches,alias:snd"`

	ID           string         `bun:"id,pk"`
	EventID      string         `bun:"event_id,notnull"`
	Projector    string         `bun:"projector,notnull"`
	Definition   string         `bun:"definition_code,notnull"`
	RecipientKey string         `bun:"recipient_key,notnull"`
	Idempotency  string         `bun:"idempotency_key,notnull"`
	Status       string         `bun:"status,notnull"`
	Error        string         `bun:"error,notnull"`
	Metadata     map[string]any `bun:"metadata,type:jsonb,notnull"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
