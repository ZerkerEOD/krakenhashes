-- Irreversible by design: restore the description, but not the clamped values.
--
-- The up migration overwrites any value above 60 with '60' and does not record
-- what was there before, so the original numbers are simply gone. Inventing one
-- here -- resetting every row to the '4' default, say -- would silently discard
-- a deliberate 15 or 30 belonging to a deployment that never had a bad value in
-- the first place. Leaving the clamped value in place is the honest no-op: it
-- is a legal setting either way, and an operator who wants a different one can
-- set it from the admin UI now that the field actually saves.
--
-- Only the description reverts, back to the text seeded by 000122.

UPDATE system_settings
SET description = 'Timeout in minutes for hashcat --keyspace and --total-candidates commands. Increase for large wordlists/rules.'
WHERE key = 'keyspace_calculation_timeout_minutes';
