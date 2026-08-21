-- Admit RunPod's two tiers as provider kinds.
--
-- WHY TWO KINDS AND NOT ONE WITH A FLAG
--
-- RunPod's own API distinguishes them with a single field (cloud: SECURE |
-- COMMUNITY), which makes "one provider, one setting" look like the natural
-- modelling. It is the wrong split, because the two tiers sit on opposite sides
-- of the only line this system cares about:
--
--   runpod            RunPod's OWN datacentres, single-tenant per host.
--                     SOC 2 Type II, ISO 27001, PCI DSS. Trust-equivalent to
--                     the operator's own AWS account.
--   runpod_community  PEER-OPERATED machines. The host's owner has root over
--                     the container, and NONE of the compliance attestations
--                     above extend to this tier -- the protection is a
--                     terms-of-service clause, not an isolation boundary.
--
-- As separate kinds an admin allowlists them independently, a client can permit
-- Secure without Community, and the consent machinery (provider-level
-- acknowledgement, per-client ack, per-job opt-in) attaches to exactly the tier
-- that needs it. As one kind plus a flag, every one of those decisions would
-- have to be re-derived from a settings blob, and the failure direction is
-- silent: a client who allowlisted "runpod" would find peer hardware inside
-- their allowlist without ever having agreed to it.
--
-- WHY THE CONSTRAINT IS DROPPED AND RE-ADDED
--
-- The original was written inline (`provider VARCHAR(32) NOT NULL CHECK (...)`)
-- in 20260813120200. Postgres names such a constraint automatically --
-- cloud_provider_configs_provider_check -- and there is no ALTER that edits a
-- CHECK in place. Editing the applied migration instead would do nothing at
-- all: golang-migrate records it as applied and never re-runs it, so the change
-- would exist in the file and not in any database that had already migrated.

ALTER TABLE cloud_provider_configs
    DROP CONSTRAINT IF EXISTS cloud_provider_configs_provider_check;

ALTER TABLE cloud_provider_configs
    ADD CONSTRAINT cloud_provider_configs_provider_check
    CHECK (provider IN ('vastai', 'aws', 'runpod', 'runpod_community', 'mock'));

COMMENT ON COLUMN cloud_provider_configs.provider IS
    'Provider kind. vastai and runpod_community are PEER-OPERATED hardware whose host owner has root over the container: they require a third-party data-exposure acknowledgement before being enabled, a per-client acknowledgement before entering an allowlist, and a per-job opt-in before receiving work. aws and runpod (Secure Cloud) are not third-party in that sense and carry none of those gates.';
