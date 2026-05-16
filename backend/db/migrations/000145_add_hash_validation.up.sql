-- Migration: Add hash validation support for GitHub issue #38.
-- Adds the invalid_hashes table, new bookkeeping columns on hashlists,
-- and relaxes the hashlists.status CHECK constraint to include the two new
-- states the validator workflow introduces ('awaiting_validation_decision',
-- 'cancelled') alongside the statuses already used by the existing code
-- ('ready_with_errors', 'deleting' — present in models/hashlist.go but never
-- declared in the original CHECK).

-- Drop the legacy CHECK constraint so we can extend the allowed set.
ALTER TABLE hashlists DROP CONSTRAINT IF EXISTS hashlists_status_check;

-- Recreate with the full set of statuses the application uses today plus the
-- two new ones for the validator workflow.
ALTER TABLE hashlists ADD CONSTRAINT hashlists_status_check
    CHECK (status IN (
        'uploading',
        'processing',
        'ready',
        'error',
        'ready_with_errors',
        'deleting',
        'awaiting_validation_decision',
        'cancelled'
    ));

-- New columns on hashlists for the validator workflow.
ALTER TABLE hashlists
    ADD COLUMN IF NOT EXISTS invalid_count     INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_input_lines INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS validation_notice TEXT;

COMMENT ON COLUMN hashlists.invalid_count IS
    'Count of lines in the source file that failed validation. 0 when validation passed or when the hash type had no validator coverage.';
COMMENT ON COLUMN hashlists.total_input_lines IS
    'Total number of non-empty, non-comment lines read from the source file.';
COMMENT ON COLUMN hashlists.validation_notice IS
    'Non-null when the declared hash type had no validator coverage. Displayed to the user as a non-blocking info banner.';

-- Table holding individual invalid lines flagged at upload time. Lines are
-- referenced by the user-facing preview dialog and the confirm/cancel
-- endpoints. Capped at 10,000 rows per hashlist by the handler to bound
-- preview cost on pathological uploads.
CREATE TABLE invalid_hashes (
    id           BIGSERIAL PRIMARY KEY,
    hashlist_id  BIGINT NOT NULL REFERENCES hashlists(id) ON DELETE CASCADE,
    line_number  INT NOT NULL,
    content      TEXT NOT NULL,
    reason       TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_invalid_hashes_hashlist ON invalid_hashes(hashlist_id);
CREATE INDEX idx_invalid_hashes_hashlist_line ON invalid_hashes(hashlist_id, line_number);

COMMENT ON TABLE invalid_hashes IS
    'Per-line validation failures captured at hashlist upload time. Cascades on hashlist delete.';
