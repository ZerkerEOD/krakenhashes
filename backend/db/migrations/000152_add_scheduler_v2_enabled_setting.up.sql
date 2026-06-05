-- Transition-mode gate for scheduler-v2.
--
-- When 'true' (default), CreateJobExecution / CreateCustomJobExecution
-- populates scheduling_units rows for newly created jobs, so the v2
-- cycle picks them up. When 'false', no units are created and the
-- legacy scheduler owns the job (via the NOT EXISTS clause in
-- GetJobsWithPendingWork).
--
-- The flag is read at JOB CREATION TIME. Flipping the setting later
-- does NOT migrate in-flight jobs between schedulers — each job keeps
-- the scheduler it was created under. This is deliberate: legacy and
-- scheduler-v2 both run side-by-side during the transition window;
-- the operator's only knob is which scheduler owns *new* jobs.
--
-- Safe to re-run (ON CONFLICT DO NOTHING). On a fresh install the
-- default is 'true' (use scheduler-v2 for everything). On upgrade,
-- existing in-flight jobs that pre-date scheduling_units remain on
-- legacy automatically; only new jobs created after upgrade get v2.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'scheduler_v2_enabled',
    'true',
    'When true, newly-created jobs are owned by scheduler-v2 (the dispatcher populates scheduling_units rows). When false, new jobs fall back to the legacy scheduler. In-flight jobs are unaffected by changes to this setting; ownership is fixed at job creation time. Both schedulers run side-by-side during the transition window.',
    'boolean'
)
ON CONFLICT (key) DO NOTHING;
