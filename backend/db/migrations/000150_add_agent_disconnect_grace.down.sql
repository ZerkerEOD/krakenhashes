-- Roll back the disconnect-grace column.

DROP INDEX IF EXISTS idx_agents_disconnect_grace;
ALTER TABLE agents DROP COLUMN IF EXISTS disconnect_grace_expires_at;
