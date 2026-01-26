-- Migration: Remove Association Attack Support

-- Remove indexes first
DROP INDEX IF EXISTS idx_association_wordlists_hashlist;

-- Remove foreign key column from job_executions
ALTER TABLE job_executions DROP COLUMN IF EXISTS association_wordlist_id;

-- Drop tables
DROP TABLE IF EXISTS association_wordlists;

-- Remove columns from hashlists
ALTER TABLE hashlists DROP COLUMN IF EXISTS has_mixed_work_factors;
ALTER TABLE hashlists DROP COLUMN IF EXISTS original_file_path;
