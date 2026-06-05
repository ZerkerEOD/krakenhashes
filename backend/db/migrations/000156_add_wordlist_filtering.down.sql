-- Revert wordlist pre-filtering columns (GH #40).

DROP INDEX IF EXISTS idx_wordlists_owner_job;
DROP INDEX IF EXISTS idx_wordlists_parent;

ALTER TABLE wordlists
    DROP COLUMN IF EXISTS is_stale,
    DROP COLUMN IF EXISTS owner_job_id,
    DROP COLUMN IF EXISTS is_ephemeral,
    DROP COLUMN IF EXISTS parent_md5,
    DROP COLUMN IF EXISTS filter_spec,
    DROP COLUMN IF EXISTS parent_wordlist_id;
