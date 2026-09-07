-- Server-side defaults for client cloud settings, and a configurable budget
-- period.
--
-- Until now every client had to be funded individually and the spend window was
-- hardcoded to a calendar month. There was no way to say "clients get $500 a
-- quarter unless I say otherwise", which is how an operator actually thinks
-- about this, and no way to fund a client at creation time.
--
-- INHERITANCE CONTRACT, matching cloud_budget_policies and
-- cloud_provisioning_rules: a per-client value that is NULL (or, for the
-- allowlist, empty) means "not configured, use the server default". A non-NULL
-- value overrides it. This is why cloud_enabled has to become nullable: a NOT
-- NULL boolean cannot distinguish "explicitly off" from "never set".
--
-- ⚠ Existing rows are explicitly false, so they stay explicitly off. Nothing is
-- retroactively funded by this migration: an operator has to set a default AND
-- the clients have to be left inheriting before anything new can spend.

-- Per-client budget period. NULL inherits cloud_default_budget_period.
ALTER TABLE clients
    ADD COLUMN IF NOT EXISTS cloud_budget_period VARCHAR(16);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'clients_cloud_budget_period_check'
    ) THEN
        ALTER TABLE clients
            ADD CONSTRAINT clients_cloud_budget_period_check
            CHECK (cloud_budget_period IS NULL
                   OR cloud_budget_period IN ('monthly', 'quarterly', 'semiannual'));
    END IF;
END $$;

COMMENT ON COLUMN clients.cloud_budget_period IS
    'Spend window this client''s budget applies to. NULL inherits cloud_default_budget_period.';

-- cloud_enabled becomes tri-state so "never configured" is expressible.
-- Existing rows keep their explicit true/false; only the column default changes
-- to NULL so newly created clients inherit rather than defaulting to off.
ALTER TABLE clients ALTER COLUMN cloud_enabled DROP DEFAULT;
ALTER TABLE clients ALTER COLUMN cloud_enabled DROP NOT NULL;
ALTER TABLE clients ALTER COLUMN cloud_enabled SET DEFAULT NULL;

COMMENT ON COLUMN clients.cloud_enabled IS
    'Whether this client may burst to paid capacity. NULL inherits cloud_default_cloud_enabled.';
COMMENT ON COLUMN clients.cloud_budget_cents IS
    'Spend ceiling for the current budget window. NULL inherits cloud_default_client_budget_cents.';
COMMENT ON COLUMN clients.cloud_provider_allowlist IS
    'Providers this client''s data may go to. Empty inherits cloud_default_provider_allowlist.';

-- Server defaults.
--
-- Seeded here because SystemSettingsRepository.SetSetting is UPDATE-only: a key
-- that was never inserted can never be written.
--
-- cloud_default_client_budget_cents is seeded NULL, not 0. NULL means no default
-- has been set, so an inheriting client remains unfunded -- the fail-closed
-- position this feature must not quietly give up. 0 would read as "a default
-- exists and it is zero", which is the same outcome by accident rather than by
-- design and is indistinguishable from a misconfiguration.
INSERT INTO system_settings (key, value, description, data_type, updated_at)
VALUES
    ('cloud_default_client_budget_cents', NULL,
     'Default spend ceiling in cents for clients that have not set their own. Unset means inheriting clients stay unfunded. Managed in Admin -> Cloud Provisioning -> Client Budgets.',
     'integer', NOW()),
    ('cloud_default_budget_period', 'monthly',
     'Spend window budgets apply to: monthly, quarterly or semiannual. Windows are calendar-aligned and computed on read, so changing this takes effect immediately and re-evaluates existing spend against the new window.',
     'string', NOW()),
    ('cloud_default_cloud_enabled', 'false',
     'Whether clients that have not set their own value may burst to paid capacity. Managed in Admin -> Cloud Provisioning -> Client Budgets.',
     'boolean', NOW()),
    ('cloud_default_provider_allowlist', NULL,
     'Comma-separated providers clients inherit when their own allowlist is empty. Peer providers (vastai, runpod_community) still require a per-client acknowledgement regardless of this default.',
     'string', NOW())
ON CONFLICT (key) DO NOTHING;
