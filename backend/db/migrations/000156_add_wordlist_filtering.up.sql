-- Wordlist pre-filtering (GH #40).
--
-- A "filtered wordlist" is a derived wordlist produced by streaming a parent
-- wordlist through a filter (min/max length, character-class requirements,
-- regex) once on the backend and materializing the matching lines into a new
-- plaintext file. Because it is a normal `wordlists` row, it flows through the
-- existing keyspace/chunking/dispatch/agent-sync pipeline unchanged.
--
-- Two modes:
--   * permanent  - created in Wordlist Management, reusable, tracks the parent
--                  (flagged stale when the parent's MD5 changes).
--   * ephemeral  - created inline on a custom job, scoped to that job, deleted
--                  when the job ends.

ALTER TABLE wordlists
    ADD COLUMN parent_wordlist_id INTEGER REFERENCES wordlists(id) ON DELETE SET NULL,
    ADD COLUMN filter_spec JSONB,
    ADD COLUMN parent_md5 VARCHAR(32),
    ADD COLUMN is_ephemeral BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN owner_job_id UUID REFERENCES job_executions(id) ON DELETE CASCADE,
    ADD COLUMN is_stale BOOLEAN NOT NULL DEFAULT false;

-- Lookups: find a parent's derived children (stale flagging, listing) and
-- find a job's ephemeral wordlist(s) for cleanup.
CREATE INDEX idx_wordlists_parent ON wordlists(parent_wordlist_id) WHERE parent_wordlist_id IS NOT NULL;
CREATE INDEX idx_wordlists_owner_job ON wordlists(owner_job_id) WHERE owner_job_id IS NOT NULL;

COMMENT ON COLUMN wordlists.parent_wordlist_id IS 'Source wordlist this filtered list was derived from (NULL for normal uploads)';
COMMENT ON COLUMN wordlists.filter_spec IS 'JSON filter criteria used to generate this filtered wordlist';
COMMENT ON COLUMN wordlists.parent_md5 IS 'Parent wordlist MD5 at generation time; used to detect parent drift';
COMMENT ON COLUMN wordlists.is_ephemeral IS 'TRUE for job-scoped filtered wordlists that are deleted when their job ends';
COMMENT ON COLUMN wordlists.owner_job_id IS 'Job execution that owns an ephemeral filtered wordlist (for cleanup)';
COMMENT ON COLUMN wordlists.is_stale IS 'TRUE when a permanent filtered wordlist''s parent has changed since generation';
