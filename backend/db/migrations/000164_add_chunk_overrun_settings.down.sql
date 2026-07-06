DELETE FROM system_settings
WHERE key IN ('chunk_overrun_guard_enabled', 'chunk_overrun_tolerance_percent');
