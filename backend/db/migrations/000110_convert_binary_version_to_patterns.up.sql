-- Binary Version Pattern Matching Migration
-- Converts binary_version_id (INT) columns to binary_version (VARCHAR) for pattern support
-- Patterns: "default", "7.x", "7.1.x", "7.1.2", "7.1.2-NTLMv3"

BEGIN;

-- ============================================================
-- AGENTS: Evolve binary_override + binary_version_id → binary_version pattern
-- The existing binary_override mechanism becomes the pattern system
-- ============================================================
ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_binary_version_id_fkey;

-- Add new column
ALTER TABLE agents ADD COLUMN binary_version VARCHAR(255) DEFAULT 'default';

-- Migrate existing data:
-- If binary_override=true AND binary_version_id is set → use that specific version
-- Otherwise → 'default' (agent accepts any binary, same as binary_override=false)
UPDATE agents SET binary_version = CASE
    WHEN binary_override = true AND binary_version_id IS NOT NULL THEN
        COALESCE((SELECT bv.version FROM binary_versions bv WHERE bv.id = agents.binary_version_id), 'default')
    ELSE 'default'
END;

-- Drop old columns (their functionality is now captured in binary_version pattern)
ALTER TABLE agents DROP COLUMN IF EXISTS binary_version_id;
ALTER TABLE agents DROP COLUMN IF EXISTS binary_override;

-- ============================================================
-- PRESET_JOBS: Convert INT → VARCHAR
-- ============================================================
ALTER TABLE preset_jobs DROP CONSTRAINT IF EXISTS preset_jobs_binary_version_id_fkey;

ALTER TABLE preset_jobs ADD COLUMN binary_version VARCHAR(255) DEFAULT 'default';

-- Migrate all presets to 'default' per user request
UPDATE preset_jobs SET binary_version = 'default';

ALTER TABLE preset_jobs DROP COLUMN IF EXISTS binary_version_id;

-- ============================================================
-- JOB_EXECUTIONS: Convert INT → VARCHAR
-- ============================================================
ALTER TABLE job_executions DROP CONSTRAINT IF EXISTS job_executions_binary_version_id_fkey;

ALTER TABLE job_executions ADD COLUMN binary_version VARCHAR(255) DEFAULT 'default';

-- Migrate existing data
UPDATE job_executions SET binary_version = COALESCE(
    (SELECT bv.version FROM binary_versions bv WHERE bv.id = job_executions.binary_version_id),
    'default'
);

ALTER TABLE job_executions DROP COLUMN IF EXISTS binary_version_id;

-- ============================================================
-- JOB_TASKS: ADD new column for resolved binary (NEW - didn't exist)
-- ============================================================
ALTER TABLE job_tasks ADD COLUMN binary_version_id INTEGER
    REFERENCES binary_versions(id) ON DELETE SET NULL;

-- ============================================================
-- Indexes
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_job_tasks_binary_version ON job_tasks(binary_version_id)
    WHERE binary_version_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_binary_versions_version ON binary_versions(version)
    WHERE is_active = true;

-- ============================================================
-- Documentation
-- ============================================================
COMMENT ON COLUMN agents.binary_version IS
    'Version pattern specifying compatible binaries. Values: "default", "7.x", "7.1.x", "7.1.2", "7.1.2-suffix"';
COMMENT ON COLUMN preset_jobs.binary_version IS
    'Version pattern template for jobs created from this preset';
COMMENT ON COLUMN job_executions.binary_version IS
    'Version pattern specifying required binary. Resolved at task creation time.';
COMMENT ON COLUMN job_tasks.binary_version_id IS
    'Resolved binary version ID assigned at task creation time';

COMMIT;
