-- Return never-configured clients to inheriting cloud_enabled.
--
-- 20260907140000 made clients.cloud_enabled nullable so that NULL could mean
-- "inherit the server default", and left every existing row at its stored
-- `false` on the reasoning that nothing should be retroactively funded.
--
-- That reasoning was wrong in one specific way, and it produced the opposite of
-- a safe default: it FROZE every client into an explicit opt-out. Setting a
-- server default then had no effect on anybody, and the only route to a working
-- configuration was opening each client in turn and flipping a dropdown --
-- which is exactly the per-client manual toggling the default was introduced to
-- remove.
--
-- The stored `false` was never a decision. It is the old column default
-- (cloud_enabled BOOLEAN NOT NULL DEFAULT false) on rows that predate cloud
-- provisioning entirely, and no operator ever chose it. Treating it as an
-- explicit opt-out grants it a meaning it never had.
--
-- Scoped to rows that show no sign of ever having been configured: no budget of
-- their own. A client someone actually set up keeps whatever it says. Nothing
-- here can spend money on its own either — an inheriting client still requires
-- the admin to explicitly enable AND fund the server default, which the UI
-- warns about at the point of doing it.
UPDATE clients
SET cloud_enabled = NULL,
    updated_at = NOW()
WHERE cloud_enabled = false
  AND cloud_budget_cents IS NULL;
