-- Drop the runtime selection index
DROP INDEX IF EXISTS idx_agent_devices_selected_runtime;

-- Remove runtime selection columns
ALTER TABLE agent_devices
DROP COLUMN IF EXISTS runtime_options,
DROP COLUMN IF EXISTS selected_runtime;
