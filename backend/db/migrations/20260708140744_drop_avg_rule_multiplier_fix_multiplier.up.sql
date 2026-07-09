-- Drop avg_rule_multiplier and fix multiplication_factor (round, not truncate).
--
-- Background: effective_keyspace (NUMERIC) + base_keyspace are the single source
-- of truth for keyspace math. Two derived "multiplier" encodings had drifted:
--   - avg_rule_multiplier (NUMERIC(20,10), added in 000063): a float ratio that
--     lost precision above 2^53, carried two contradictory definitions (full
--     multiplier at creation vs. a residual on the benchmark path), and was never
--     read by the frontend. Removed — every correctness path now derives the
--     ratio from effective/base in big.Int (overflow/precision-safe).
--   - multiplication_factor (BIGINT): the display-only "×N" chip value. It was
--     computed with INTEGER TRUNCATION of effective/base, so base×2.9999 rendered
--     as ×2 instead of ×3. Kept, but now recomputed as round(effective/base).
--
-- Backfill corrects existing rows (e.g. the 5600/3-salt job: 2 → 3). round()
-- yields a whole numeric, so the implicit cast to BIGINT is exact. The ratio
-- (rules × salts) is bounded well under the BIGINT ceiling; GREATEST(1, …)
-- mirrors the code's floor guard.

-- 1. Correct the existing display-only multiplication_factor values.
UPDATE job_executions
SET multiplication_factor = GREATEST(1, round(effective_keyspace::numeric / base_keyspace))
WHERE base_keyspace IS NOT NULL AND base_keyspace > 0
  AND effective_keyspace IS NOT NULL AND effective_keyspace > 0;

UPDATE preset_jobs
SET multiplication_factor = GREATEST(1, round(effective_keyspace::numeric / keyspace))
WHERE keyspace IS NOT NULL AND keyspace > 0
  AND effective_keyspace IS NOT NULL AND effective_keyspace > 0;

-- 2. Drop the redundant, imprecise column.
ALTER TABLE job_executions DROP COLUMN IF EXISTS avg_rule_multiplier;
