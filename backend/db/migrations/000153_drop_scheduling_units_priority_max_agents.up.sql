-- Drop the denormalized priority and max_agents columns from
-- scheduling_units. They were copies of job_executions.priority and
-- .max_agents populated once at unit creation and never resynced —
-- which meant that editing the job in the admin UI changed the
-- job_executions row but left the scheduling_units row with the
-- original value, silently feeding stale data into the v2 allocator.
--
-- After this migration the scheduler's buildUnitInfos JOINs
-- job_executions live for the same fields, eliminating the drift
-- entirely. See plan: drop-denormalized-priority-max-agents.
--
-- The partial index idx_scheduling_units_dispatch (priority DESC,
-- created_at) drops automatically because Postgres cascades index
-- removal when its leading column disappears. The replacement query
-- in GetSchedulable orders on job_executions.priority via the JOIN;
-- if profiling shows the new ORDER BY is slow, add a partial index
-- on scheduling_units (created_at) WHERE status IN ('pending','running')
-- AND is_accurate_keyspace = true in a follow-up.

ALTER TABLE scheduling_units DROP COLUMN IF EXISTS priority;
ALTER TABLE scheduling_units DROP COLUMN IF EXISTS max_agents;
