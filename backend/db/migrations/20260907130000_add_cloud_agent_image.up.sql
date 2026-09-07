-- Admin-managed container image for cloud agents.
--
-- This replaces KH_CLOUD_AGENT_IMAGE as the source of truth, for the same
-- reason the TLS SAN settings replaced their environment variables: the value
-- was read once at startup into a service field, so changing it meant editing a
-- file on the host and restarting the backend. Choosing which build runs on
-- rented hardware is an operator decision, and it belongs in the UI next to the
-- spend ceilings that bound it.
--
-- It also fails badly when wrong. The compiled default points at
-- zerkereod/krakenhashes-agent-cloud:latest, which does not exist until a
-- release is tagged; the branch builds publish :dev and :cloud-gpu instead. An
-- unresolvable image is not a startup error — the instance boots, `docker pull`
-- fails, the bootstrap disarms the deadline and the host terminates itself
-- about a minute later. You are billed for that minute and the only evidence is
-- a line in the instance's system log, which is gone with the instance.
--
-- Seeded NULL, not '', because the two are distinguishable through the
-- repository's *string return and the distinction is the migration mechanism:
--
--   NULL     -> never configured; KH_CLOUD_AGENT_IMAGE still applies and is
--               imported into this row on the next boot.
--   non-NULL -> the database is authoritative and the environment variable is
--               ignored from then on. The empty string is a valid configured
--               value meaning "fall back to the compiled default".
--
-- Seeded here because SystemSettingsRepository.SetSetting is UPDATE-only: a key
-- that was never inserted can never be written.
INSERT INTO system_settings (key, value, description, data_type, updated_at)
VALUES
    ('cloud_agent_image', NULL,
     'Container image cloud agents run, e.g. zerkereod/krakenhashes-agent-cloud:latest. Rented instances pull this from a registry, so it must be a tag that actually exists — a missing tag bills for roughly a minute and self-terminates. Managed in Admin -> Cloud Provisioning -> Limits & operations.',
     'string', NOW())
ON CONFLICT (key) DO NOTHING;
