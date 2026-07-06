-- Slice D.1: correct the description on speed_test_timeout_seconds_compressed.
-- The migration 000140 description listed several extensions hashcat does not
-- actually support (.gzip / .bz2 / .zst / .7z). Verified against hashcat 7.x
-- src/filehandling.c — only .gz, .zip, .xz are recognised by magic-byte
-- detection. This UPDATE keeps the seeded value untouched and only refreshes
-- the human-facing description so admins reading system_settings get the
-- accurate list.

UPDATE system_settings
SET description = 'Speed-test timeout in seconds for compressed wordlists (.gz / .zip / .xz). These need significantly longer to dictstat-preprocess than plain text wordlists, especially when the file is large (e.g. rockyou2021.txt.gz is ~26 GB). Hashcat does not natively read bzip2, zstd, or 7z wordlists.'
WHERE key = 'speed_test_timeout_seconds_compressed';
