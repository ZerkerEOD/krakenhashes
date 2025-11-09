-- Add runtime selection columns to agent_devices table
ALTER TABLE agent_devices
ADD COLUMN runtime_options JSONB NOT NULL DEFAULT '[]'::jsonb,
ADD COLUMN selected_runtime VARCHAR(50);

-- Migrate existing data
-- Assume existing devices are OpenCL with their current device_id
UPDATE agent_devices
SET selected_runtime = 'OpenCL',
    runtime_options = jsonb_build_array(
        jsonb_build_object(
            'backend', 'OpenCL',
            'device_id', device_id,
            'processors', 0,
            'clock', 0,
            'memory_total', 0,
            'memory_free', 0,
            'pci_address', ''
        )
    )
WHERE runtime_options = '[]'::jsonb;

-- Add helpful comments
COMMENT ON COLUMN agent_devices.device_id IS 'Physical device index (0-based position), not hashcat device ID';
COMMENT ON COLUMN agent_devices.runtime_options IS 'JSONB array of available runtimes with their hashcat device IDs';
COMMENT ON COLUMN agent_devices.selected_runtime IS 'Currently selected runtime: CUDA, HIP, or OpenCL';

-- Create index on selected_runtime for faster filtering
CREATE INDEX idx_agent_devices_selected_runtime ON agent_devices(selected_runtime);
