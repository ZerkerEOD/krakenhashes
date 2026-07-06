-- System settings for the agent auto-update pipeline.
--
--   agent_auto_update_enabled            Master on/off. When false, no agent
--                                        is ever told to update (stale agents
--                                        simply keep running their version).
--   agent_update_max_concurrent          Cap on how many agents may be in the
--                                        'updating' state at once, so the
--                                        server's binary-serving bandwidth is
--                                        not pegged when a whole fleet is stale.
--   agent_update_health_timeout_seconds  How long an agent may stay 'updating'
--                                        before the sweeper declares the update
--                                        failed (it should reconnect on the new
--                                        version well within this window).
--   agent_update_max_attempts            Give-up threshold: after this many
--                                        failed attempts the agent is left in
--                                        'error' and no longer retried
--                                        automatically (admin can retry).
--
-- Safe to re-run (ON CONFLICT DO NOTHING). Defaults: auto-update ON.

INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('agent_auto_update_enabled', 'true',
     'Master toggle for automatic agent auto-updates. When off, no agent is told to update.',
     'boolean'),
    ('agent_update_max_concurrent', '2',
     'Maximum number of agents allowed in the updating state at once, capping binary-serving bandwidth.',
     'integer'),
    ('agent_update_health_timeout_seconds', '300',
     'Seconds an agent may remain in the updating state before it is declared failed if it has not reconnected on the target version.',
     'integer'),
    ('agent_update_max_attempts', '3',
     'After this many failed update attempts the agent is left in error state and no longer retried automatically.',
     'integer')
ON CONFLICT (key) DO NOTHING;
