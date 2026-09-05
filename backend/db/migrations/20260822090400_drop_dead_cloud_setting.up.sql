-- Remove a cloud setting that configures nothing.
--
-- cloud_ttl_extension_max_pct describes a feature that does not exist: nothing
-- in the codebase extends an instance's TTL, so the value an admin picks here
-- has never had any effect and cannot until that feature is written.
--
-- A settings row is a promise that the number does something. A knob that
-- silently does nothing is worse than an absent one, because an operator who
-- tunes it believes they have changed the system's behaviour. The remaining
-- cloud_* settings are all read in internal/services/cloud/settings.go.
--
-- If TTL extension is implemented later, reintroduce this key in the migration
-- that adds the feature, so the two arrive together.

DELETE FROM system_settings WHERE key = 'cloud_ttl_extension_max_pct';
