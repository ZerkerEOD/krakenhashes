-- System settings for the chunk-overrun guard.
--
--   chunk_overrun_guard_enabled       When on, the scheduler stops a task that
--                                     has run past its chunk-time target and
--                                     re-dispatches the remainder, feeding the
--                                     measured speed back so future chunks for
--                                     that (agent, hash type, attack mode) are
--                                     sized correctly. When off, a long-running
--                                     chunk is left to finish on its own.
--   chunk_overrun_tolerance_percent   Grace window before the guard fires: it
--                                     stops a task once wall time exceeds
--                                     chunk_duration × (1 + this/100). Default
--                                     20% absorbs small speed fluctuations.
--
-- Safe to re-run (ON CONFLICT DO NOTHING). Defaults: guard ON, 20% tolerance.

INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('chunk_overrun_guard_enabled', 'true',
     'When on, the scheduler stops a task that has run past its chunk-time target (× the tolerance below), re-dispatches the remainder, and feeds the measured speed back so future chunks are sized correctly.',
     'boolean'),
    ('chunk_overrun_tolerance_percent', '20',
     'Grace window before the chunk-overrun guard stops a task: it fires once wall time exceeds chunk_duration × (1 + this/100). Default 20 percent.',
     'integer')
ON CONFLICT (key) DO NOTHING;
