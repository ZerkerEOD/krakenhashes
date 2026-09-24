-- Admin-managed subject alternative names for the server certificate.
--
-- These replace KH_ADDITIONAL_IP_ADDRESSES / KH_ADDITIONAL_DNS_NAMES as the
-- source of truth. Those variables were only ever read when the certificate was
-- first generated, so changing them after the first boot did nothing at all --
-- which left deployments with a certificate covering only 127.0.0.1 and every
-- agent unable to complete a TLS handshake.
--
-- Seeded here because SystemSettingsRepository.SetSetting is UPDATE-only: a key
-- that was never inserted can never be written.
--
-- The value is seeded NULL, not ''. The two are distinguishable through the
-- repository's *string return, and the distinction is the migration mechanism:
--
--   NULL     -> never configured; the legacy environment variable still applies
--               and is imported into this row on the next boot.
--   non-NULL -> the database is authoritative and the environment variable is
--               ignored from then on, including when set to the empty string.
--
-- Values are comma-separated, matching the format of the environment variables
-- they replace so the one-time import is a straight copy.
INSERT INTO system_settings (key, value, description, data_type, updated_at)
VALUES
    ('tls_additional_ip_addresses', NULL,
     'Extra IP addresses to include in the server certificate, comma-separated. Only private and VPN addresses are accepted (RFC1918, CGNAT 100.64/10 for Tailscale/NetBird, link-local, IPv6 ULA); public addresses are rejected because KrakenHashes is an internal-only tool. Managed in Admin -> Settings -> Server Certificate.',
     'string', NOW()),
    ('tls_additional_dns_names', NULL,
     'Extra DNS names to include in the server certificate, comma-separated. Managed in Admin -> Settings -> Server Certificate.',
     'string', NOW())
ON CONFLICT (key) DO NOTHING;
