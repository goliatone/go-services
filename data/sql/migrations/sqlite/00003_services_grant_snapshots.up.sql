CREATE TABLE IF NOT EXISTS service_grant_snapshots (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0),
    requested_grants TEXT NOT NULL DEFAULT '[]',
    granted_grants TEXT NOT NULL DEFAULT '[]',
    metadata TEXT NOT NULL DEFAULT '{}',
    captured_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(connection_id, version)
);

CREATE INDEX IF NOT EXISTS idx_service_grant_snapshots_connection_id
    ON service_grant_snapshots(connection_id);
CREATE INDEX IF NOT EXISTS idx_service_grant_snapshots_provider_id
    ON service_grant_snapshots(provider_id);
CREATE INDEX IF NOT EXISTS idx_service_grant_snapshots_created_at
    ON service_grant_snapshots(created_at);

INSERT OR IGNORE INTO service_grant_snapshots (
    id,
    connection_id,
    provider_id,
    scope_type,
    scope_id,
    version,
    requested_grants,
    granted_grants,
    metadata,
    captured_at,
    created_at,
    updated_at
)
SELECT
    connection_id || ':' || COALESCE(json_extract(metadata, '$.snapshot_version'), '1') AS id,
    connection_id,
    provider_id,
    scope_type,
    scope_id,
    COALESCE(CAST(json_extract(metadata, '$.snapshot_version') AS INTEGER), 1) AS version,
    requested_grants,
    granted_grants,
    metadata,
    created_at,
    created_at,
    created_at
FROM service_grant_events
WHERE event_type = 'snapshot';
