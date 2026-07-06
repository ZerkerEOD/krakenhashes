-- Roll back per-agent benchmark health tracking.

DELETE FROM system_settings WHERE key IN (
    'benchmark_hard_failure_cap',
    'agent_benchmark_quarantine_streak',
    'agent_benchmark_quarantine_distinct',
    'agent_benchmark_streak_reset_minutes',
    'benchmark_storm_threshold',
    'benchmark_storm_window_minutes'
);

DROP INDEX IF EXISTS idx_agents_benchmark_failure_streak;

ALTER TABLE agents
    DROP COLUMN IF EXISTS benchmark_failure_streak,
    DROP COLUMN IF EXISTS benchmark_distinct_combos_failed,
    DROP COLUMN IF EXISTS benchmark_last_failure_at;
