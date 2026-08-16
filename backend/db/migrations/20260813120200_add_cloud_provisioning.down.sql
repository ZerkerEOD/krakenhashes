-- Reverse 20260813120200_add_cloud_provisioning.
--
-- Order matters: agents.cloud_instance_id references cloud_instances, and the
-- ledger references both cloud_instances and clients, so the referencing
-- columns and tables come down before the tables they point at.
--
-- NOTE: this does not terminate anything. If rented instances are live when
-- this is rolled back, the rows that track them are destroyed and the
-- instances become unreconcilable orphans that will bill until their in-guest
-- TTL watchdog fires. Drain the fleet before rolling back.

ALTER TABLE agents
    DROP COLUMN IF EXISTS cloud_instance_id,
    DROP COLUMN IF EXISTS retired_at;

ALTER TABLE job_workflows
    DROP COLUMN IF EXISTS cloud_burst_enabled;

ALTER TABLE preset_jobs
    DROP COLUMN IF EXISTS cloud_burst_enabled,
    DROP COLUMN IF EXISTS cloud_max_instances;

ALTER TABLE job_executions
    DROP COLUMN IF EXISTS cloud_burst_enabled,
    DROP COLUMN IF EXISTS cloud_max_instances;

ALTER TABLE clients
    DROP COLUMN IF EXISTS cloud_enabled,
    DROP COLUMN IF EXISTS cloud_provider_allowlist,
    DROP COLUMN IF EXISTS cloud_budget_cents,
    DROP COLUMN IF EXISTS max_instance_ttl_minutes,
    DROP COLUMN IF EXISTS provider_ack;

DROP TABLE IF EXISTS cloud_gpu_benchmarks;
DROP TABLE IF EXISTS cloud_spend_ledger;
DROP TABLE IF EXISTS cloud_instances;
DROP TABLE IF EXISTS cloud_budget_policies;
DROP TABLE IF EXISTS cloud_provider_configs;

DELETE FROM system_settings WHERE key IN (
    'cloud_global_monthly_cap_cents',
    'cloud_global_concurrent_instance_cap',
    'cloud_chunk_duration_seconds',
    'cloud_teardown_slack_seconds',
    'cloud_idle_drain_minutes',
    'cloud_ttl_extension_max_pct',
    'cloud_reaper_interval_seconds',
    'cloud_orphan_grace_minutes',
    'scheduler_endgame_tapering_enabled',
    'endgame_threshold_multiple'
);
