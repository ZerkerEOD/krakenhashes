-- Migration: Add teams_enabled system setting
-- Purpose: Feature toggle to enable/disable multi-team access control
-- Default: FALSE (single-team mode, current behavior preserved)

INSERT INTO system_settings (key, value, description, data_type, updated_at)
VALUES (
    'teams_enabled',
    'false',
    'Enable multi-team access control. When enabled, users only see clients/hashlists/jobs assigned to their teams. When disabled, all users see all resources (current behavior).',
    'boolean',
    NOW()
)
ON CONFLICT (key) DO NOTHING;
-- Note: DO NOTHING is intentional for idempotent migrations.
-- If the setting already exists (e.g., from a previous partial migration),
-- we preserve the existing value rather than overwriting it.

-- Add comment for documentation
COMMENT ON COLUMN system_settings.key IS 'Setting key - teams_enabled controls multi-team mode';
