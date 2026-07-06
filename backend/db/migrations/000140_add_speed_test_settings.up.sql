-- Speed-test (benchmark) admin settings.
-- Replaces the hardcoded 30s TestDuration with two timeouts that admins can
-- tune live, plus a configurable minimum number of status updates the agent
-- collects before returning a result.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'speed_test_timeout_seconds_uncompressed',
    '120',
    'Speed-test (benchmark) timeout in seconds for plain-text wordlists. Hashcat needs time to autotune the GPUs and produce its first --status-json reading; if the timeout fires before any non-zero speed is reported, the benchmark is failed.',
    'integer'
)
ON CONFLICT (key) DO NOTHING;

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'speed_test_timeout_seconds_compressed',
    '300',
    'Speed-test timeout in seconds for compressed wordlists (.gz / .gzip / .bz2 / .zst / .7z / .zip). These need significantly longer to dictstat-preprocess than plain text wordlists, especially when the file is large (e.g. rockyou2021.txt.gz is ~26 GB).',
    'integer'
)
ON CONFLICT (key) DO NOTHING;

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'speed_test_min_status_updates',
    '3',
    'Minimum number of hashcat --status-json ticks the agent must collect before reporting a benchmark result. Minimum 1. Values of 1 or 2 may produce inaccurate readings because the GPUs have not finished spinning up.',
    'integer'
)
ON CONFLICT (key) DO NOTHING;
