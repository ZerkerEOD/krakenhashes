-- Expand documented values for agent_overflow_allocation_mode to cover
-- the two new Max-Agents overflow variants (max_agents_fifo,
-- max_agents_round_robin). Schema unchanged — the value column accepts
-- any string. This migration only refreshes the description text so
-- that operators querying system_settings see all five options.
--
-- Existing deployments keep their current value (fifo / round_robin /
-- enforce_max_agents) untouched.

UPDATE system_settings
SET description = 'Overflow allocation policy when free agents remain after each tier hits max_agents. Five values: ''fifo'' (Priority - FIFO: tier-local; surplus piles on the oldest unit at the highest active priority tier, exceeding its cap; lower tiers may starve), ''round_robin'' (Priority - Round Robin: tier-local; rotates within the highest active tier, exceeding caps; lower tiers may starve), ''enforce_max_agents'' (strict; every tier fills to cap, surplus agents stay idle), ''max_agents_fifo'' (Max Agents - FIFO: every tier fills to cap first, then surplus piles on the OLDEST job overall by created_at, ignoring priority), ''max_agents_round_robin'' (Max Agents - Round Robin: every tier fills to cap first, then surplus rotates evenly across ALL jobs, ignoring priority).'
WHERE key = 'agent_overflow_allocation_mode';
