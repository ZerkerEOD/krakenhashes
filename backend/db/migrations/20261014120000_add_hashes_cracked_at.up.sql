-- Add a dedicated crack-time column so the Cracked Hashes (/pot) list can order by
-- when a hash was actually cracked. last_updated is unreliable for this: a BEFORE
-- UPDATE trigger rewrites it to NOW() on every row change, so it reflects the last
-- modification, not the crack time. cracked_at is set once when a hash is cracked and
-- is never touched again (the trigger only rewrites last_updated).
ALTER TABLE hashes ADD COLUMN IF NOT EXISTS cracked_at TIMESTAMPTZ;

-- Best-effort backfill for existing cracked hashes (last_updated is the closest proxy).
UPDATE hashes SET cracked_at = last_updated WHERE is_cracked = true AND cracked_at IS NULL;

-- Supports the cracked-hash queries ordering by crack time.
CREATE INDEX IF NOT EXISTS idx_hashes_cracked_at ON hashes (cracked_at DESC) WHERE is_cracked = true;
