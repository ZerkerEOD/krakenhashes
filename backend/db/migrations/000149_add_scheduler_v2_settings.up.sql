-- New tunables for the scheduler rewrite. All have sensible defaults so the
-- system runs out of the box; operators tune in the admin UI. The
-- task_heartbeat_timeout_setting from migration 000046 is preserved
-- (currently unread); this migration adds explicit per-phase defaults the
-- rewrite reads directly. The new key is parallel rather than a rename so
-- the migration is fully additive.
--
-- The agent_overflow_allocation_mode setting (existing) gains a third value
-- 'enforce_max_agents'; the description is updated to reflect the three
-- options. We do this with a conditional UPDATE so re-running is safe and
-- old deployments that already have the setting in fifo / round_robin mode
-- keep their selection.

INSERT INTO system_settings (key, value, description, data_type)
VALUES
    ('task_heartbeat_timeout_seconds',
     '120',
     'Seconds without any liveness signal (progress update, liveness ping, task_loading message, or new outfile crack) before a running task is considered lost and gap-recovered.',
     'integer'),
    ('task_startup_grace_seconds',
     '600',
     'Pre-first-progress grace window after a task is started, covering file downloads, wordlist decompression, and hashcat autotune. The heartbeat timer does not start until either a progress update arrives or this grace window passes.',
     'integer'),
    ('network_grace_seconds',
     '30',
     'WebSocket reconnect tolerance. A running task whose agent disconnects is given this many seconds to reconnect before recovery (truncate-and-gap) fires.',
     'integer'),
    ('target_chunk_seconds',
     '60',
     'Target wall time for a single task chunk on the assigned agent, in seconds. The dispatcher sizes chunks as agent_speed * this value, clamped by the unit''s remaining gap.',
     'integer'),
    ('min_chunk_seconds',
     '5',
     'Minimum chunk wall time. Gaps smaller than agent_speed * this value are dispatched whole rather than skipped (prevents one-candidate orphans).',
     'integer')
ON CONFLICT (key) DO NOTHING;

-- Update the overflow mode setting's description to enumerate the three
-- valid values. The value column is left as-is (deployed operators keep
-- their selection); 'enforce_max_agents' becomes valid going forward.
UPDATE system_settings
SET description = 'Distribution policy for agents in excess of per-job max_agents at the highest active priority tier. Values: ''fifo'' (oldest job at tier gets all overflow), ''round_robin'' (rotate one agent at a time across tier jobs), or ''enforce_max_agents'' (strict cap at every tier; surplus descends to the next priority tier).'
WHERE key = 'agent_overflow_allocation_mode';
