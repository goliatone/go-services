CREATE TABLE IF NOT EXISTS service_mapping_specs (
    id TEXT PRIMARY KEY,
    spec_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    source_object TEXT NOT NULL,
    target_model TEXT NOT NULL,
    schema_ref TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL,
    rules TEXT NOT NULL DEFAULT '[]',
    metadata TEXT NOT NULL DEFAULT '{}',
    published_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (provider_id, scope_type, scope_id, spec_id, version)
);

CREATE INDEX IF NOT EXISTS idx_service_mapping_specs_scope
    ON service_mapping_specs(provider_id, scope_type, scope_id);
CREATE INDEX IF NOT EXISTS idx_service_mapping_specs_status
    ON service_mapping_specs(status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_service_mapping_specs_published_spec
    ON service_mapping_specs(provider_id, scope_type, scope_id, spec_id)
    WHERE status = 'published';

CREATE TABLE IF NOT EXISTS service_sync_bindings (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    mapping_spec_id TEXT NOT NULL REFERENCES service_mapping_specs(id) ON DELETE CASCADE,
    source_object TEXT NOT NULL,
    target_model TEXT NOT NULL,
    direction TEXT NOT NULL,
    status TEXT NOT NULL,
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (connection_id, mapping_spec_id, direction)
);

CREATE INDEX IF NOT EXISTS idx_service_sync_bindings_connection_id
    ON service_sync_bindings(connection_id);
CREATE INDEX IF NOT EXISTS idx_service_sync_bindings_scope
    ON service_sync_bindings(provider_id, scope_type, scope_id);
CREATE INDEX IF NOT EXISTS idx_service_sync_bindings_status
    ON service_sync_bindings(status);

CREATE TABLE IF NOT EXISTS service_sync_checkpoints (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    sync_binding_id TEXT NOT NULL REFERENCES service_sync_bindings(id) ON DELETE CASCADE,
    direction TEXT NOT NULL,
    cursor TEXT NOT NULL DEFAULT '',
    sequence_num INTEGER NOT NULL DEFAULT 0 CHECK (sequence_num >= 0),
    source_version TEXT NOT NULL DEFAULT '',
    idempotency_seed TEXT NOT NULL DEFAULT '',
    metadata TEXT NOT NULL DEFAULT '{}',
    last_event_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (sync_binding_id, direction, sequence_num)
);

CREATE INDEX IF NOT EXISTS idx_service_sync_checkpoints_binding_direction
    ON service_sync_checkpoints(sync_binding_id, direction, updated_at DESC);

CREATE TABLE IF NOT EXISTS service_identity_bindings (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    sync_binding_id TEXT NOT NULL REFERENCES service_sync_bindings(id) ON DELETE CASCADE,
    source_object TEXT NOT NULL,
    external_id TEXT NOT NULL,
    internal_type TEXT NOT NULL,
    internal_id TEXT NOT NULL,
    match_kind TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (sync_binding_id, external_id)
);

CREATE INDEX IF NOT EXISTS idx_service_identity_bindings_internal
    ON service_identity_bindings(sync_binding_id, internal_type, internal_id);

CREATE TABLE IF NOT EXISTS service_sync_conflicts (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    sync_binding_id TEXT NOT NULL REFERENCES service_sync_bindings(id) ON DELETE CASCADE,
    checkpoint_id TEXT REFERENCES service_sync_checkpoints(id) ON DELETE SET NULL,
    source_object TEXT NOT NULL,
    external_id TEXT NOT NULL,
    source_version TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    policy TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL,
    status TEXT NOT NULL,
    source_payload TEXT NOT NULL DEFAULT '{}',
    target_payload TEXT NOT NULL DEFAULT '{}',
    resolution TEXT NOT NULL DEFAULT '{}',
    resolved_by TEXT NOT NULL DEFAULT '',
    resolved_at DATETIME,
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_sync_conflicts_binding_status
    ON service_sync_conflicts(sync_binding_id, status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_service_sync_conflicts_idempotency
    ON service_sync_conflicts(sync_binding_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE TABLE IF NOT EXISTS service_sync_change_log (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    sync_binding_id TEXT NOT NULL REFERENCES service_sync_bindings(id) ON DELETE CASCADE,
    direction TEXT NOT NULL,
    source_object TEXT NOT NULL,
    external_id TEXT NOT NULL,
    source_version TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    metadata TEXT NOT NULL DEFAULT '{}',
    occurred_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_sync_change_log_binding_direction_time
    ON service_sync_change_log(sync_binding_id, direction, occurred_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_service_sync_change_log_idempotency
    ON service_sync_change_log(sync_binding_id, idempotency_key)
    WHERE idempotency_key <> '';
