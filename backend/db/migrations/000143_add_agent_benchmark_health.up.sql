-- Per-agent benchmark health tracking. Distinct from `consecutive_failures`
-- (which counts task execution failures, not benchmark failures).
--
-- Rationale: when one agent in a fleet keeps failing benchmarks across multiple
-- (hash_type, attack_mode) combos, the operator wants that box auto-quarantined
-- so siblings keep running. The per-(agent, job, mode, hash_type) blocklist
-- alone can't express "this whole agent is sick."
ALTER TABLE agents
    ADD COLUMN benchmark_failure_streak         INTEGER     NOT NULL DEFAULT 0,
    ADD COLUMN benchmark_distinct_combos_failed INTEGER     NOT NULL DEFAULT 0,
    ADD COLUMN benchmark_last_failure_at        TIMESTAMPTZ;

CREATE INDEX idx_agents_benchmark_failure_streak
    ON agents(benchmark_failure_streak)
    WHERE benchmark_failure_streak > 0;

-- Tunables, all with the same INSERT...ON CONFLICT DO NOTHING idiom used by
-- migration 000139 so re-running is safe.
INSERT INTO system_settings (key, value, description, data_type)
VALUES
    ('benchmark_hard_failure_cap',
     '10',
     'Per-(agent, job, attack_mode, hash_type) failures before the job is auto-marked failed regardless of other attribution evidence.',
     'integer'),
    ('agent_benchmark_quarantine_streak',
     '15',
     'Per-agent consecutive benchmark failures before auto-quarantine (sets is_enabled=false and notifies).',
     'integer'),
    ('agent_benchmark_quarantine_distinct',
     '3',
     'Per-agent DISTINCT (hash_type, attack_mode) combos failed within the streak window before auto-quarantine.',
     'integer'),
    ('agent_benchmark_streak_reset_minutes',
     '60',
     'Minutes of benchmark inactivity that reset the per-agent streak (avoids false quarantine after a long idle).',
     'integer'),
    ('benchmark_storm_threshold',
     '5',
     'Minimum benchmark failures on the same job within the storm window before a benchmark_storm informational notification is emitted to admins.',
     'integer'),
    ('benchmark_storm_window_minutes',
     '15',
     'Time window (in minutes) used to detect a benchmark storm on a single job.',
     'integer')
ON CONFLICT (key) DO NOTHING;
