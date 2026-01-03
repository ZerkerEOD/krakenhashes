-- Add multiplication_factor to preset_jobs for pre-calculated rule multiplier
-- This enables job creation to be a pure copy operation when is_accurate_keyspace = true

ALTER TABLE preset_jobs
ADD COLUMN IF NOT EXISTS multiplication_factor INT NOT NULL DEFAULT 1;

COMMENT ON COLUMN preset_jobs.multiplication_factor IS 'Rule multiplier (effective_keyspace / keyspace) for rule splitting decisions';
