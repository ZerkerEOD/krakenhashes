-- Reverses 20260907150000_clients_inherit_cloud_enabled.up.sql.
--
-- Inheriting clients go back to an explicit false rather than being left NULL:
-- rolling back means the inheritance machinery may be going away, and the only
-- safe reading of "inherit" without it is "off". A rollback must never be the
-- reason a client gains the ability to spend.
UPDATE clients
SET cloud_enabled = false,
    updated_at = NOW()
WHERE cloud_enabled IS NULL;
