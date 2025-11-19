-- Add system setting for agent overflow allocation mode
-- This controls how agents beyond max_agents limits are allocated
-- when multiple jobs have the same priority level

INSERT INTO system_settings (key, value, description, data_type, updated_at)
VALUES (
    'agent_overflow_allocation_mode',
    'fifo',
    'Agent allocation beyond max_agents at same priority. Options: fifo (oldest job gets extras) or round_robin (distribute evenly across jobs)',
    'string',
    NOW()
)
ON CONFLICT (key) DO NOTHING;
