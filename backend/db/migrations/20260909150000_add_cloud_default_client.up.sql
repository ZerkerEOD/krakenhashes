-- A billing client for work that has none.
--
-- Cloud money is tracked per client everywhere: cloud_spend_ledger.client_id,
-- the budget window, the threshold ladder, the spend report. Provisioning
-- therefore needs a client, and both entry points joined clients directly --
-- so a hashlist with no client matched no row and could never rent capacity,
-- with no message at all on the autoscaler path.
--
-- That assumed every deployment models a billing entity. Many do not: a
-- single-org or internal install has no "client" to assign, and
-- require_client_for_hashlist only gates NEW uploads, so it neither helps the
-- hashlists that already exist nor suits a team that legitimately has no
-- clients to name.
--
-- The fallback is a real client the admin nominates rather than a synthetic
-- NULL bucket, so there stays exactly ONE budget code path instead of a second
-- one guarded by NULL checks throughout the spend SQL -- and unassigned spend
-- shows up as an ordinary row in the client budget UI and the spend report,
-- which a non-client could not. Naming that client "Unassigned Work" makes the
-- report read honestly.
--
-- Seeded EMPTY, which means no fallback: behaviour is unchanged until an admin
-- nominates a client. The join stays an INNER JOIN, so an unset, malformed or
-- since-deleted value resolves to NULL, matches nothing, and refuses to rent --
-- the same outcome as before this existed. Letting a job through with no budget
-- holder is the one result worse than not provisioning.
--
-- Seeded rather than left absent because SystemSettingsRepository.SetSetting is
-- UPDATE-only: a key that does not exist here can never be written by an admin.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'cloud_default_client_id',
    '',
    'Client that cloud spend is billed to when a hashlist has no client assigned. Empty means unassigned work cannot use cloud, which is the default. Intended for deployments that do not model clients; budgets, caps and the threshold ladder all apply to the nominated client as normal.',
    'string'
)
ON CONFLICT (key) DO NOTHING;
