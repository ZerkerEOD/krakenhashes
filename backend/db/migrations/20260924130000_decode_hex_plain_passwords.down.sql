-- Irreversible by design: the up migration replaced $HEX[...] literals with the
-- decoded passwords and did not keep the originals. Re-encoding every password
-- that happens to contain ':' would also rewrite passwords that were never
-- hex-encoded, so no automatic reversal is attempted. Nothing to do.
SELECT 1;
