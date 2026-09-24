-- Restore the description from 20260822090200_add_cloud_provisioning.up.sql.
--
-- The key itself is NOT deleted. The up migration only created it in the
-- pathological case where it was already missing, and the pre-fix dispatcher
-- reads the same key -- deleting it would leave a rolled-back deployment with a
-- setting the admin UI can never write again (SetSetting is UPDATE-only).

UPDATE system_settings
SET description = 'Target chunk duration for cloud agents, ~3x the on-prem default_chunk_duration. Per-chunk overhead (hashcat startup, kernel autotune, wordlist load) is billed at rental rates. Always clamped to the instance''s remaining TTL.'
WHERE key = 'cloud_chunk_duration_seconds';
