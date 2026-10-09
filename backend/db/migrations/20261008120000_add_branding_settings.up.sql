-- Application branding (GitHub issue #41).
--
-- Admin-configurable display name, browser page title, accent colours and the
-- file names of an uploaded logo / favicon. The "powered by KrakenHashes"
-- attribution is NOT stored: the backend appends it to the effective page title
-- on every read and strips it from the stored title on every write, so it can
-- never be removed or doubled through the settings API.
--
-- Seeded here because SystemSettingsRepository.SetSetting is UPDATE-only: a key
-- that was never inserted can never be written. Every value is seeded NULL,
-- meaning "not configured" -- the defaults (KrakenHashes name, red accent,
-- bundled emblem) apply until an admin sets something.
--
-- branding_logo_file / branding_favicon_file hold a generated basename inside
-- <KH_DATA_DIR>/branding/ (e.g. logo-<uuid>.png), never a user-supplied path.
INSERT INTO system_settings (key, value, description, data_type, updated_at)
VALUES
    ('branding_app_name', NULL,
     'Custom application name shown in the UI, emails, TOTP issuer and PDF reports. NULL = KrakenHashes. Managed in Admin -> Settings -> System Settings -> Branding.',
     'string', NOW()),
    ('branding_page_title', NULL,
     'Custom browser page title base. The "powered by KrakenHashes" suffix is always appended by the server. NULL = application name.',
     'string', NOW()),
    ('branding_primary_color', NULL,
     'Primary accent colour as #rrggbb. NULL = #ff0000.',
     'string', NOW()),
    ('branding_secondary_color', NULL,
     'Secondary accent colour as #rrggbb. NULL = none (Material-UI default).',
     'string', NOW()),
    ('branding_logo_file', NULL,
     'Generated file name of the uploaded logo inside the branding data directory. NULL = bundled KrakenHashes emblem.',
     'string', NOW()),
    ('branding_favicon_file', NULL,
     'Generated file name of the uploaded favicon inside the branding data directory. NULL = bundled favicon.',
     'string', NOW())
ON CONFLICT (key) DO NOTHING;
