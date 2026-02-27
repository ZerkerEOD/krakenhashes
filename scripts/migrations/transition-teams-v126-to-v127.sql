-- ============================================================================
-- Transition Script: Teams Feature Branch -> Merged Master
-- ============================================================================
--
-- PURPOSE: One-time migration for testers who ran the feature/teams-implementation
-- branch (schema_migrations version=126 with old numbering).
--
-- BACKGROUND:
--   The feature branch had team migrations numbered 000122-000126. After merging
--   with master, they were renumbered to 000123-000127 to make room for master's
--   000122_add_keyspace_timeout_setting. Since golang-migrate only tracks the
--   version number (not migration filenames), the team tables are already correct.
--   This script just applies the missing keyspace timeout setting and bumps the
--   version to 127.
--
-- HOW TO RUN:
--   docker exec -i krakenhashes-postgres psql -U krakenhashes -d krakenhashes \
--     < scripts/migrations/transition-teams-v126-to-v127.sql
--
-- SAFE TO RE-RUN: Yes. The INSERT uses ON CONFLICT DO NOTHING, and the version
-- check prevents double-application.
-- ============================================================================

DO $$
DECLARE
    current_version INTEGER;
    is_dirty BOOLEAN;
BEGIN
    -- Read current migration state
    SELECT version, dirty INTO current_version, is_dirty FROM schema_migrations;

    -- Guard: dirty state requires manual intervention
    IF is_dirty THEN
        RAISE EXCEPTION 'Database is in dirty state (version=%, dirty=true). Fix the dirty state manually before running this transition script.', current_version;
    END IF;

    -- Guard: already transitioned
    IF current_version >= 127 THEN
        RAISE NOTICE 'Database is already at version % (>= 127). No transition needed.', current_version;
        RETURN;
    END IF;

    -- Guard: unexpected version
    IF current_version <> 126 THEN
        RAISE EXCEPTION 'Expected schema_migrations version=126 (teams feature branch), but found version=%. This script only applies to databases that were running the feature/teams-implementation branch.', current_version;
    END IF;

    -- Step 1: Apply master's migration 122 content (keyspace timeout setting)
    INSERT INTO system_settings (key, value, description, data_type, updated_at)
    VALUES (
        'keyspace_calculation_timeout_minutes',
        '4',
        'Timeout in minutes for hashcat --keyspace and --total-candidates commands. Increase for large wordlists/rules.',
        'integer',
        NOW()
    )
    ON CONFLICT (key) DO NOTHING;

    -- Step 2: Bump version from 126 to 127
    UPDATE schema_migrations SET version = 127 WHERE version = 126;

    RAISE NOTICE 'Transition complete:';
    RAISE NOTICE '  - keyspace_calculation_timeout_minutes setting applied';
    RAISE NOTICE '  - schema_migrations version updated: 126 -> 127';
    RAISE NOTICE '  - You can now start the merged version of the application.';
END $$;
