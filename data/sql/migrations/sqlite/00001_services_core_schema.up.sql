CREATE TABLE IF NOT EXISTS service_connections (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    external_account_id TEXT NOT NULL,
    status TEXT NOT NULL,
    inherits_from_connection_id TEXT REFERENCES service_connections(id) ON DELETE SET NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_service_connections_provider_id ON service_connections(provider_id);
CREATE INDEX IF NOT EXISTS idx_service_connections_scope ON service_connections(scope_type, scope_id);
CREATE INDEX IF NOT EXISTS idx_service_connections_external_account_id ON service_connections(external_account_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_service_connections_active_scope
    ON service_connections(provider_id, scope_type, scope_id, external_account_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS service_credentials (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    encrypted_payload BLOB NOT NULL,
    token_type TEXT NOT NULL,
    requested_scopes TEXT NOT NULL DEFAULT '[]',
    granted_scopes TEXT NOT NULL DEFAULT '[]',
    expires_at DATETIME,
    rotates_at DATETIME,
    refreshable INTEGER NOT NULL DEFAULT 0 CHECK (refreshable IN (0, 1)),
    status TEXT NOT NULL,
    grant_version INTEGER NOT NULL DEFAULT 1,
    encryption_key_id TEXT NOT NULL DEFAULT '',
    encryption_version INTEGER NOT NULL DEFAULT 1,
    revocation_reason TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(connection_id, version)
);

CREATE INDEX IF NOT EXISTS idx_service_credentials_connection_id ON service_credentials(connection_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_service_credentials_active
    ON service_credentials(connection_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS service_events (
    id TEXT PRIMARY KEY,
    connection_id TEXT REFERENCES service_connections(id) ON DELETE SET NULL,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_events_connection_id ON service_events(connection_id);
CREATE INDEX IF NOT EXISTS idx_service_events_provider_id ON service_events(provider_id);
CREATE INDEX IF NOT EXISTS idx_service_events_scope ON service_events(scope_type, scope_id);
CREATE INDEX IF NOT EXISTS idx_service_events_created_at ON service_events(created_at);

CREATE TABLE IF NOT EXISTS service_activity_entries (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    connection_id TEXT REFERENCES service_connections(id) ON DELETE SET NULL,
    installation_id TEXT,
    subscription_id TEXT,
    sync_job_id TEXT,
    channel TEXT NOT NULL,
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    status TEXT NOT NULL,
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_activity_entries_provider_id ON service_activity_entries(provider_id);
CREATE INDEX IF NOT EXISTS idx_service_activity_entries_scope ON service_activity_entries(scope_type, scope_id);
CREATE INDEX IF NOT EXISTS idx_service_activity_entries_connection_id ON service_activity_entries(connection_id);
CREATE INDEX IF NOT EXISTS idx_service_activity_entries_installation_id ON service_activity_entries(installation_id);
CREATE INDEX IF NOT EXISTS idx_service_activity_entries_subscription_id ON service_activity_entries(subscription_id);
CREATE INDEX IF NOT EXISTS idx_service_activity_entries_sync_job_id ON service_activity_entries(sync_job_id);
CREATE INDEX IF NOT EXISTS idx_service_activity_entries_channel ON service_activity_entries(channel);
CREATE INDEX IF NOT EXISTS idx_service_activity_entries_action ON service_activity_entries(action);
CREATE INDEX IF NOT EXISTS idx_service_activity_entries_created_at ON service_activity_entries(created_at);

CREATE TABLE IF NOT EXISTS service_lifecycle_outbox (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE,
    event_name TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    connection_id TEXT REFERENCES service_connections(id) ON DELETE SET NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    metadata TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME,
    last_error TEXT NOT NULL DEFAULT '',
    occurred_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_lifecycle_outbox_event_name ON service_lifecycle_outbox(event_name);
CREATE INDEX IF NOT EXISTS idx_service_lifecycle_outbox_provider_id ON service_lifecycle_outbox(provider_id);
CREATE INDEX IF NOT EXISTS idx_service_lifecycle_outbox_scope ON service_lifecycle_outbox(scope_type, scope_id);
CREATE INDEX IF NOT EXISTS idx_service_lifecycle_outbox_connection_id ON service_lifecycle_outbox(connection_id);
CREATE INDEX IF NOT EXISTS idx_service_lifecycle_outbox_status_next_attempt
    ON service_lifecycle_outbox(status, next_attempt_at);

CREATE TABLE IF NOT EXISTS service_notification_dispatches (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    projector TEXT NOT NULL,
    definition_code TEXT NOT NULL,
    recipient_key TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_notification_dispatches_event_id
    ON service_notification_dispatches(event_id);
CREATE INDEX IF NOT EXISTS idx_service_notification_dispatches_projector
    ON service_notification_dispatches(projector);
CREATE INDEX IF NOT EXISTS idx_service_notification_dispatches_recipient_key
    ON service_notification_dispatches(recipient_key);

CREATE TABLE IF NOT EXISTS service_grant_events (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    requested_grants TEXT NOT NULL DEFAULT '[]',
    granted_grants TEXT NOT NULL DEFAULT '[]',
    added_grants TEXT NOT NULL DEFAULT '[]',
    removed_grants TEXT NOT NULL DEFAULT '[]',
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_grant_events_connection_id ON service_grant_events(connection_id);
CREATE INDEX IF NOT EXISTS idx_service_grant_events_provider_id ON service_grant_events(provider_id);
CREATE INDEX IF NOT EXISTS idx_service_grant_events_created_at ON service_grant_events(created_at);

CREATE TABLE IF NOT EXISTS service_webhook_deliveries (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME,
    payload BLOB,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(provider_id, delivery_id)
);

CREATE INDEX IF NOT EXISTS idx_service_webhook_deliveries_provider_status
    ON service_webhook_deliveries(provider_id, status, next_attempt_at);

CREATE TABLE IF NOT EXISTS service_subscriptions (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    remote_subscription_id TEXT,
    callback_url TEXT NOT NULL,
    verification_token_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    expires_at DATETIME,
    metadata TEXT NOT NULL DEFAULT '{}',
    last_notified_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_service_subscriptions_connection_id ON service_subscriptions(connection_id);
CREATE INDEX IF NOT EXISTS idx_service_subscriptions_provider_resource
    ON service_subscriptions(provider_id, resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_service_subscriptions_status_expires_at
    ON service_subscriptions(status, expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_service_subscriptions_active_channel
    ON service_subscriptions(provider_id, channel_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS service_sync_cursors (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    cursor TEXT NOT NULL,
    status TEXT NOT NULL,
    last_synced_at DATETIME,
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(connection_id, provider_id, resource_type, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_service_sync_cursors_provider_resource
    ON service_sync_cursors(provider_id, resource_type, resource_id);

CREATE TABLE IF NOT EXISTS service_installations (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    install_type TEXT NOT NULL,
    status TEXT NOT NULL,
    granted_at DATETIME,
    revoked_at DATETIME,
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_installations_provider_id ON service_installations(provider_id);
CREATE INDEX IF NOT EXISTS idx_service_installations_scope ON service_installations(scope_type, scope_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_service_installations_active
    ON service_installations(provider_id, scope_type, scope_id, install_type)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS service_rate_limit_state (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    bucket_key TEXT NOT NULL,
    "limit" INTEGER NOT NULL,
    remaining INTEGER NOT NULL,
    reset_at DATETIME,
    retry_after INTEGER,
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_rate_limit_state_provider_scope
    ON service_rate_limit_state(provider_id, scope_type, scope_id);
CREATE INDEX IF NOT EXISTS idx_service_rate_limit_state_bucket_key
    ON service_rate_limit_state(bucket_key);

CREATE TABLE IF NOT EXISTS service_sync_jobs (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    mode TEXT NOT NULL,
    checkpoint TEXT,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME,
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_sync_jobs_connection_id ON service_sync_jobs(connection_id);
CREATE INDEX IF NOT EXISTS idx_service_sync_jobs_provider_status ON service_sync_jobs(provider_id, status, next_attempt_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_service_sync_jobs_active
    ON service_sync_jobs(connection_id, provider_id, mode)
    WHERE status IN ('queued', 'running');
