ALTER TABLE service_credentials
    ADD COLUMN IF NOT EXISTS payload_format TEXT NOT NULL DEFAULT 'legacy_token';

ALTER TABLE service_credentials
    ADD COLUMN IF NOT EXISTS payload_version INTEGER NOT NULL DEFAULT 1;
