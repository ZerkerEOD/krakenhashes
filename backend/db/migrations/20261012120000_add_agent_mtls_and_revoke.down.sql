DROP INDEX IF EXISTS idx_agents_client_cert_serial;

DROP TABLE IF EXISTS revoked_cert_serials;

ALTER TABLE agents
    DROP COLUMN IF EXISTS client_cert_serial,
    DROP COLUMN IF EXISTS revoked_by,
    DROP COLUMN IF EXISTS revoked_at,
    DROP COLUMN IF EXISTS is_revoked;
