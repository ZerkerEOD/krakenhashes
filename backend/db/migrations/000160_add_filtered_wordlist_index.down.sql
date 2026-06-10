-- Revert the incremental regeneration index for filtered wordlists (GH #40 follow-up).

ALTER TABLE wordlists
    DROP COLUMN IF EXISTS parent_anchor_md5,
    DROP COLUMN IF EXISTS parent_offset;
