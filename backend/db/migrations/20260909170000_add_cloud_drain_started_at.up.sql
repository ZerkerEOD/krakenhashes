-- drain_pct's clock.
--
-- The drain rung promised "stop giving it new work, let in-flight work finish,
-- then destroy" -- and delivered none of it. It wrote
-- cloud_instances.state = 'draining' and stopped there: nothing read the state,
-- so getIdleAgents kept handing the instance new chunks, and
-- drain_timeout_seconds was stored, validated and read by no service. The
-- ladder was effectively 95% stop-new-launches -> 100% kill mid-chunk, with a
-- cosmetic rung in between.
--
-- A dedicated column rather than reusing an existing timestamp: the reaper
-- bumps updated_at on every accrual (AddIncurredCost runs each sweep), so a
-- drain clock measured from it would reset itself every 60 seconds and never
-- expire. terminated_at is only written for terminal states, and ready_at must
-- not move or the commissioning grace breaks.
--
-- Persisted rather than held in memory -- the OPPOSITE call from the reaper's
-- orphanFirstSeen map, and for the opposite reason. Forgetting an orphan clock
-- across a restart merely delays a destruction, which is the safe direction.
-- Forgetting THIS one means an instance at >=99% of its client's cap keeps
-- billing for another full timeout after every restart, and a crash-looping
-- backend never destroys it at all.

ALTER TABLE cloud_instances
    ADD COLUMN IF NOT EXISTS drain_started_at TIMESTAMPTZ;

COMMENT ON COLUMN cloud_instances.drain_started_at IS
    'When the budget ladder put this instance on the drain rung. Cleared if spend falls back below drain_pct. cloud_budget_policies.drain_timeout_seconds is measured from here.';
