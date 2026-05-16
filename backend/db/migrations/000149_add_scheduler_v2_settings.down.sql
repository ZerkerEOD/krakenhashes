-- Roll back the new tunables. The agent_overflow_allocation_mode description
-- update is intentionally NOT reverted: we don't store the prior description
-- and the new wording is harmless on a rolled-back deploy.

DELETE FROM system_settings WHERE key IN (
    'task_heartbeat_timeout_seconds',
    'task_startup_grace_seconds',
    'network_grace_seconds',
    'target_chunk_seconds',
    'min_chunk_seconds'
);
