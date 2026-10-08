-- Reverse of 20260929120000_add_agent_storage_tier.up.sql.
ALTER TABLE agents
    DROP COLUMN IF EXISTS network_share_mount_path,
    DROP COLUMN IF EXISTS network_share_id,
    DROP COLUMN IF EXISTS storage_tier;
