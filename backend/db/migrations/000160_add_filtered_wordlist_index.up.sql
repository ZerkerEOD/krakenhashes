-- Incremental regeneration index for filtered wordlists (GH #40 follow-up).
--
-- When a parent wordlist changes, its permanent filtered children are flagged
-- stale and auto-regenerated. The common change is "words appended to the end"
-- of the parent. To avoid re-streaming a 25GB+ parent on every append, each
-- child records a small index describing how much of the parent it has already
-- consumed, so regeneration can filter only the new tail and APPEND the matches
-- (preserving order, so keyspace/dispatch ordering is unaffected).
--
--   parent_offset      - plaintext byte offset of the parent up to the last
--                        COMPLETE line consumed into this child at last
--                        successful generation. NULL until first generation,
--                        and for compressed parents (no seekable offset) where
--                        we always do a full rebuild.
--   parent_anchor_md5  - MD5 of the parent bytes in the 1 MiB window ending at
--                        parent_offset, captured at last generation. A cheap
--                        proof the prefix has not shifted: if the new parent's
--                        bytes in that same window still hash to this value and
--                        the file only grew, the change is an append and we can
--                        safely filter+append just [parent_offset, EOF).
--
-- parent_md5 (added in 000156) continues to hold the parent's FULL MD5 at last
-- generation and still drives stale-flagging.

ALTER TABLE wordlists
    ADD COLUMN parent_offset BIGINT,
    ADD COLUMN parent_anchor_md5 VARCHAR(32);

COMMENT ON COLUMN wordlists.parent_offset IS 'Plaintext byte offset (last complete line) of the parent consumed into this filtered child at last generation; enables incremental append regeneration';
COMMENT ON COLUMN wordlists.parent_anchor_md5 IS 'MD5 of the 1 MiB parent window ending at parent_offset, used to verify an append-only change before incremental regeneration';
