-- Per-agent storage tier (feature/network-share-storage, WS5).
--
-- storage_tier controls how an agent obtains wordlists/rules:
--   full_cache     — download over HTTP and keep everything (default; today's behavior)
--   on_demand      — download per task over HTTP, LRU-evict under disk pressure
--   network_direct — read immutable wordlists/rules directly off a mounted share
--
-- Default 'full_cache' keeps every existing agent behaving exactly as before.
-- network_share_mount_path is the agent-side read-only mount path used by the
-- network_direct tier. network_share_id is an optional informational link to
-- the (singleton) network_shares config; no FK, since the share is global.
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS storage_tier VARCHAR(20) NOT NULL DEFAULT 'full_cache'
        CHECK (storage_tier IN ('full_cache', 'on_demand', 'network_direct')),
    ADD COLUMN IF NOT EXISTS network_share_id UUID,
    ADD COLUMN IF NOT EXISTS network_share_mount_path TEXT NOT NULL DEFAULT '';
