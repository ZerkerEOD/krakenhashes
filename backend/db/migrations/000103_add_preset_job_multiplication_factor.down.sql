-- Remove multiplication_factor from preset_jobs

ALTER TABLE preset_jobs
DROP COLUMN IF EXISTS multiplication_factor;
