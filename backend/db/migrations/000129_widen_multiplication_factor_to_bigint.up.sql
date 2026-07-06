-- Widen multiplication_factor from INT to BIGINT
-- For salted hash types, multiplication_factor = rule_count × salt_count
-- which easily exceeds INT max (2,147,483,647).
-- Example: 48,414 rules × 82,780 salts = 4,008,069,004

ALTER TABLE job_executions ALTER COLUMN multiplication_factor TYPE BIGINT;
ALTER TABLE preset_jobs ALTER COLUMN multiplication_factor TYPE BIGINT;
