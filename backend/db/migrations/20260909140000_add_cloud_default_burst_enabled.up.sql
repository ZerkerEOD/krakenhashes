-- A server-wide default for "this job may rent cloud capacity".
--
-- job_executions.cloud_burst_enabled is per job and defaults false, which is
-- the right shape when cloud burst extends a fleet you already own: renting is
-- the exception, so it should be opted into one job at a time.
--
-- It is exactly backwards for a deployment with no on-prem GPUs, where renting
-- is the ONLY way work ever runs. There, the flag has to be ticked on every
-- preset job, every workflow and every ad-hoc job, and the failure mode of
-- missing one is indistinguishable from a broken install: the job sits at
-- pending and nothing anywhere says why. That is the single most likely way a
-- first-time cloud-only setup is abandoned.
--
-- Resolved inside cloudEligibilityPredicate rather than stamped onto rows at
-- job creation, so that turning it on also frees the jobs already queued --
-- which is precisely the state an operator is in at the moment they work out
-- they needed it. It also keeps one copy of the rule for both provisioning
-- entry points.
--
-- Ships false: this is a switch that causes money to be spent, so it must be a
-- deliberate act. The client budget, the per-job instance cap and the
-- deployment-wide cap all still apply on top of it -- this opts a job into
-- being CONSIDERED for renting, it does not bypass any limit.
--
-- Seeded rather than left absent because SystemSettingsRepository.SetSetting is
-- UPDATE-only: a key that does not exist here can never be written by an admin.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'cloud_default_burst_enabled',
    'false',
    'Treat every job as cloud-burst enabled, without ticking the box on each preset, workflow and job. Intended for deployments with no on-prem GPUs, where renting is the only way work runs. Client budgets and instance caps still apply.',
    'boolean'
)
ON CONFLICT (key) DO NOTHING;
