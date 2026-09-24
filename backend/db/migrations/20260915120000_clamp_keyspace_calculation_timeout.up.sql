-- keyspace_calculation_timeout_minutes is bounded at 60 minutes; clamp rows above it.
--
-- This key is MINUTES (000122 seeds '4'; both getters do
-- time.Duration(val) * time.Minute). It renders in the Keyspace & Benchmark
-- panel immediately beside two agent speed-test timeouts measured in SECONDS,
-- one of which commonly holds 3600 -- so a value like 1200 reads as "20
-- minutes" at a glance and is in fact 20 HOURS.
--
-- Three things had to line up for a bad value to persist, and all three did:
--
--  1. The field's HTML max was only ever an advisory hint. inputProps min/max
--     on <input type="number"> does not prevent typing an out-of-range value,
--     it only fails constraint validation, which the form never checked.
--  2. beea8b36 rewrote the page onto NumberSetting and RAISED this field's max
--     from 60 to 1440, so 20 hours began rendering as perfectly in range.
--  3. NumberSetting could not save at all: onChange wrote the typed value into
--     the shared values map, then onBlur guarded on
--     `next === values[settingKey]`, which was therefore always true. An
--     operator who got a wrong value in had no way to correct it -- the page
--     has no save button -- and no error either, because nothing was attempted.
--
-- (2) and (3) are fixed in the same change as this migration. This handles the
-- data already written, which must be clamped rather than left alone: with the
-- ceiling back at 60, a stored 1200 renders out of range on load, and the
-- operator would be looking at a value the form will now refuse to accept.
--
-- Deliberately scoped to value > 60 rather than resetting to the '4' default.
-- Somebody who chose 15 or 30 made a real choice about their wordlist size and
-- keeps it; only values the UI could never legitimately have produced move.
--
-- The comparison is wrapped in CASE rather than written as
-- `value ~ '^[0-9]+$' AND value::int > 60`, because SQL does not guarantee that
-- AND short-circuits -- the planner may evaluate the cast against a row the
-- regex was meant to exclude. CASE does guarantee ordering. The column is TEXT
-- and nothing constrains its contents, so a non-numeric value here is possible,
-- and a cast error would abort the migration and leave schema_migrations DIRTY
-- with the backend refusing to start. ::numeric rather than ::int for the same
-- reason: a row of 30 digits satisfies the regex and would overflow int4.
--
-- Nothing is lost by clamping. When this timeout expires a straight attack
-- falls back to the wordlist word count and notifies the user, so the practical
-- difference between 60 minutes and 20 hours is only how long a wedged hashcat
-- keeps a background goroutine before that fallback runs.

UPDATE system_settings
SET value = '60',
    updated_at = NOW()
WHERE key = 'keyspace_calculation_timeout_minutes'
  AND CASE
        WHEN value ~ '^[0-9]+$' THEN value::numeric > 60
        ELSE FALSE
      END;

UPDATE system_settings
SET description = 'Timeout in MINUTES (max 60) for hashcat --keyspace and --total-candidates commands. Note the agent speed-test timeouts next to this one are in SECONDS. On expiry a straight attack falls back to the wordlist word count and notifies the user.'
WHERE key = 'keyspace_calculation_timeout_minutes';
