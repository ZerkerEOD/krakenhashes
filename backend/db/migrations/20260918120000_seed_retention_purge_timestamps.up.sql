-- Seed the purge timestamp keys so the retention service can actually write them.
--
-- ClientSettingsRepository.SetSetting is UPDATE-only: it reports
-- "client setting with key 'X' not found for update" when no row exists, and
-- nothing ever inserted these. Both purge paths therefore failed on every run
-- -- four errors per cycle in the logs of the reference deployment -- and the
-- timestamp they exist to record was never stored.
--
-- Two keys, not one. The hashlist retention purge and the analytics report
-- purge both wrote 'last_purge_run', so even once the row existed each would
-- overwrite the other and neither "when did retention last run" question could
-- be answered. 'last_purge_run' keeps its name for the hashlist purge because
-- it is the one already documented; the analytics purge moves to its own key.
--
-- Seeded empty rather than with a timestamp: no purge has run yet, and a
-- fabricated date would read as one that had.

INSERT INTO client_settings (key, value, description)
VALUES (
    'last_purge_run',
    NULL,
    'Timestamp (RFC3339) of the last data retention purge of hashlists. NULL until the first purge runs.'
)
ON CONFLICT (key) DO NOTHING;

INSERT INTO client_settings (key, value, description)
VALUES (
    'last_analytics_purge_run',
    NULL,
    'Timestamp (RFC3339) of the last analytics report retention purge. NULL until the first purge runs.'
)
ON CONFLICT (key) DO NOTHING;
