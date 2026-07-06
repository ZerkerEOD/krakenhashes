-- Roll back benchmark failure attribution.

DELETE FROM system_settings WHERE key IN (
    'benchmark_blocklist_cooldown_hours',
    'benchmark_failure_threshold'
);

ALTER TABLE agent_benchmark_history DROP COLUMN IF EXISTS source;

DROP TABLE IF EXISTS agent_benchmark_blocklist;
DROP TABLE IF EXISTS benchmark_failure_attempts;
