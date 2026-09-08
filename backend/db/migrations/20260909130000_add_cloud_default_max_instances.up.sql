-- A blank per-job cloud_max_instances must not mean "unlimited".
--
-- job_executions.cloud_max_instances is nullable and NULL has always meant
-- unbounded. That was defensible while two other brakes engaged:
--
--   * the autoscaler refuses to rent while any on-prem agent sits idle, and
--   * skip_if_finishing_within_seconds skips a job about to complete anyway.
--
-- Both are computed from ON-PREM agents. In a cloud-only deployment there are
-- none, so idleOnPrem is permanently 0 and the finishing-soon projection has no
-- throughput to reason from — both brakes are structurally disengaged. A job
-- also keeps publishing as "starving" for the entire hour its one instance
-- works a chunk, because starvation means "received no new allocation this
-- cycle". The result is one rental per 60-second tick, bounded only by the
-- client budget.
--
-- Blank is also the default and the path of least resistance in the UI, so the
-- uncapped case is the one an operator falls into rather than chooses.
--
-- 2 is deliberately conservative. It is a DEFAULT, not a ceiling: a per-job
-- value still overrides it, and an operator who wants wider fan-out raises this
-- once. Erring low costs some parallelism; erring high costs money that is
-- already spent by the time anyone looks.
--
-- Seeded rather than left absent because SystemSettingsRepository.SetSetting is
-- UPDATE-only: a key that does not exist here can never be written by an admin.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'cloud_default_max_instances_per_job',
    '2',
    'Maximum rented instances for a job that does not set its own limit. Applies only when a job leaves "max cloud instances" blank. Set to 0 for unlimited, which is bounded only by the client budget.',
    'integer'
)
ON CONFLICT (key) DO NOTHING;
