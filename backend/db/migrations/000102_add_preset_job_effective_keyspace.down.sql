-- Remove effective keyspace and rule splitting fields from preset_jobs

ALTER TABLE preset_jobs
DROP COLUMN IF EXISTS use_rule_splitting,
DROP COLUMN IF EXISTS is_accurate_keyspace,
DROP COLUMN IF EXISTS effective_keyspace;
