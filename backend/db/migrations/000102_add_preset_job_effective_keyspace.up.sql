-- Add effective keyspace and rule splitting fields to preset_jobs
-- These enable accurate keyspace calculation using --total-candidates
-- and pre-computation of rule splitting decisions

ALTER TABLE preset_jobs
ADD COLUMN IF NOT EXISTS effective_keyspace BIGINT,
ADD COLUMN IF NOT EXISTS is_accurate_keyspace BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS use_rule_splitting BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN preset_jobs.keyspace IS 'Base keyspace from hashcat --keyspace (wordlist line count)';
COMMENT ON COLUMN preset_jobs.effective_keyspace IS 'Actual effective keyspace from hashcat --total-candidates (accounts for rules)';
COMMENT ON COLUMN preset_jobs.is_accurate_keyspace IS 'TRUE if effective_keyspace came from --total-candidates, FALSE if estimated';
COMMENT ON COLUMN preset_jobs.use_rule_splitting IS 'TRUE if jobs from this preset should use rule splitting (calculated at preset creation)';
