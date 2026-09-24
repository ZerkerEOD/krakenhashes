-- Backfill retired_at for cloud agents whose instance is long gone.
--
-- Reaper.finalize retired the agent only when cloud_instances.agent_id was
-- populated:
--
--     if inst.AgentID != nil { RetireAgent(*inst.AgentID) }
--
-- When that back-reference was null the retirement was skipped silently, so the
-- agent row stayed un-retired forever -- visible in the agent list and, until
-- the scheduler's retired_at filter was the only thing excluding it, eligible
-- to be handed a chunk on a machine that no longer exists. Three agents on the
-- reference deployment survived their terminated instances exactly that way.
--
-- The code fix is RetireAgentsForInstance, which keys on the AGENT's own
-- cloud_instance_id instead. This migration cleans up the rows that already
-- escaped.
--
-- Scoped deliberately:
--   * cloud_instance_id IS NOT NULL  -- never touches an on-prem agent
--   * the instance is terminal       -- never retires an agent still working
--   * retired_at IS NULL             -- idempotent, preserves existing stamps
UPDATE agents a
   SET retired_at = COALESCE(ci.terminated_at, ci.updated_at, NOW()),
       updated_at = NOW()
  FROM cloud_instances ci
 WHERE a.cloud_instance_id = ci.id
   AND a.retired_at IS NULL
   AND ci.state IN ('terminated', 'failed');
