DROP TABLE IF EXISTS service_sync_job_idempotency;

CREATE UNIQUE INDEX IF NOT EXISTS uq_service_sync_jobs_active
    ON service_sync_jobs(connection_id, provider_id, mode)
    WHERE status IN ('queued', 'running');
