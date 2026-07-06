-- Revert to the original description seeded in migration 000140. The value
-- field is left as-is in both directions because it's the source of truth
-- the backend reads at runtime; only description is informational.

UPDATE system_settings
SET description = 'Speed-test timeout in seconds for compressed wordlists (.gz / .gzip / .bz2 / .zst / .7z / .zip). These need significantly longer to dictstat-preprocess than plain text wordlists, especially when the file is large (e.g. rockyou2021.txt.gz is ~26 GB).'
WHERE key = 'speed_test_timeout_seconds_compressed';
