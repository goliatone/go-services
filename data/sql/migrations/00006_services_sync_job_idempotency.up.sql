DROP INDEX IF EXISTS uq_service_sync_jobs_active;

CREATE TABLE IF NOT EXISTS service_sync_job_idempotency (
    id TEXT PRIMARY KEY,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES service_connections(id) ON DELETE CASCADE,
    mode TEXT NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (btrim(idempotency_key) <> ''),
    sync_job_id TEXT NOT NULL REFERENCES service_sync_jobs(id) ON DELETE CASCADE,
    requested_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_service_sync_job_idempotency_scope_provider
    ON service_sync_job_idempotency(scope_type, scope_id, provider_id);
CREATE INDEX IF NOT EXISTS idx_service_sync_job_idempotency_sync_job_id
    ON service_sync_job_idempotency(sync_job_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_service_sync_job_idempotency_tuple
    ON service_sync_job_idempotency(scope_type, scope_id, provider_id, mode, idempotency_key);
