WITH ranked AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY provider_id, scope_type, scope_id, bucket_key
            ORDER BY updated_at DESC, id ASC
        ) AS row_num
    FROM service_rate_limit_state
)
DELETE FROM service_rate_limit_state AS srls
USING ranked
WHERE srls.id = ranked.id
  AND ranked.row_num > 1;

CREATE UNIQUE INDEX IF NOT EXISTS uq_service_rate_limit_state_natural_key
    ON service_rate_limit_state(provider_id, scope_type, scope_id, bucket_key);
