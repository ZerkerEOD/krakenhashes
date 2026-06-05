-- Restore the scheduler_v2_enabled setting with its transition-mode
-- default. Note: simply restoring the row does NOT revive the legacy
-- scheduler runner — that requires code changes (see job_integration.go
-- StartScheduler). This down-migration exists only to preserve schema
-- symmetry, not as a functional rollback path.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'scheduler_v2_enabled',
    'true',
    'When true, newly-created jobs are owned by scheduler-v2 (the dispatcher populates scheduling_units rows). When false, new jobs fall back to the legacy scheduler. In-flight jobs are unaffected by changes to this setting; ownership is fixed at job creation time. Both schedulers run side-by-side during the transition window.',
    'boolean'
)
ON CONFLICT (key) DO NOTHING;
