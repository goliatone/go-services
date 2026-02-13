ALTER TABLE service_credentials
    ADD COLUMN payload_format TEXT NOT NULL DEFAULT 'legacy_token';

ALTER TABLE service_credentials
    ADD COLUMN payload_version INTEGER NOT NULL DEFAULT 1;
