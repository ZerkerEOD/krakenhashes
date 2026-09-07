-- Reverses 20260907140000_add_cloud_budget_defaults.up.sql.
--
-- Restoring NOT NULL on cloud_enabled requires resolving the tri-state first.
-- A NULL there means "inherit", and the only safe reading when inheritance is
-- being removed is "off": funding a client as a side effect of a rollback is
-- the one outcome this must not produce.
UPDATE clients SET cloud_enabled = false WHERE cloud_enabled IS NULL;

ALTER TABLE clients ALTER COLUMN cloud_enabled SET DEFAULT false;
ALTER TABLE clients ALTER COLUMN cloud_enabled SET NOT NULL;

ALTER TABLE clients DROP CONSTRAINT IF EXISTS clients_cloud_budget_period_check;
ALTER TABLE clients DROP COLUMN IF EXISTS cloud_budget_period;

DELETE FROM system_settings
WHERE key IN (
    'cloud_default_client_budget_cents',
    'cloud_default_budget_period',
    'cloud_default_cloud_enabled',
    'cloud_default_provider_allowlist'
);
