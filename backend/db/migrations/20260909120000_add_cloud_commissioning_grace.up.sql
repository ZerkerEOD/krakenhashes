-- Split the cloud reaper's single teardown clock into two.
--
-- cloud_idle_drain_minutes (5) governed BOTH "this instance is between chunks"
-- and "this instance has never been given a task". Those want different numbers
-- by roughly six times, and using the small one for both destroyed healthy
-- instances during their own cold start: a rented instance must finish a file
-- sync (~467 MB of hashcat, bounded by the scheduler at 10 minutes) and then a
-- benchmark before any job_tasks row exists, and the reaper's activity signal
-- is derived only from job_tasks.
--
-- That alone would be waste. What made it a loop is that the job was still
-- starving, so the autoscaler rented a replacement on the next tick, which met
-- the same fate. Neither dead-on-arrival breaker catches it, because both key
-- on ready_at IS NULL and an instance killed during sync has ready_at set.
--
-- 30 minutes is chosen to clear scheduler.ReadinessBudget() (20 minutes: the
-- sync gate failing open, plus one benchmark and one stale-window retry) with
-- margin. A test asserts the relationship so moving either constant fails CI
-- rather than silently reopening the loop.
--
-- Seeded rather than left absent because SystemSettingsRepository.SetSetting is
-- UPDATE-only: a key that does not exist here can never be written by an admin.
--
-- Zero disables commissioning teardown, matching cloud_idle_drain_minutes.
-- NOTE this is a semantic change to that older setting: cloud_idle_drain_minutes
-- = 0 no longer keeps a never-commissioned instance alive forever, because that
-- case is now governed by this key instead.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'cloud_commissioning_grace_minutes',
    '30',
    'Destroy a rented instance that has never been given a task after this many minutes, measured from when its agent registered. Must exceed the time a healthy agent needs to sync files and run its first benchmark (~20 minutes) or healthy instances are torn down mid-startup and immediately re-rented. Set to 0 to disable.',
    'integer'
)
ON CONFLICT (key) DO NOTHING;
