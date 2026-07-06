-- Restore the description set by migration 000149.

UPDATE system_settings
SET description = 'Distribution policy for agents in excess of per-job max_agents at the highest active priority tier. Values: ''fifo'' (oldest job at tier gets all overflow), ''round_robin'' (rotate one agent at a time across tier jobs), or ''enforce_max_agents'' (strict cap at every tier; surplus descends to the next priority tier).'
WHERE key = 'agent_overflow_allocation_mode';
