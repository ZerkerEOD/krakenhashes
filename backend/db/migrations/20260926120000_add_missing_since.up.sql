-- GH #93: the directory monitor reconciles disk -> DB only, so a wordlist or
-- rule whose file disappears from disk (a manual rm, a moved data dir, a lost
-- volume) keeps its 'verified' row forever. Every agent then 404s on it during
-- bulk sync and lands on sync_status='failed', which reads as an agent health
-- problem rather than one missing resource.
--
-- The reconcile pass marks such a row 'failed', but 'failed' is already used for
-- other reasons (a filtered wordlist whose generation failed). missing_since
-- distinguishes "failed because the file is gone" from those, so the UI can say
-- so and the pass only auto-restores rows it flagged for absence (never a
-- genuine content/generation failure). NULL = not flagged missing.

ALTER TABLE wordlists ADD COLUMN missing_since TIMESTAMPTZ;
ALTER TABLE rules     ADD COLUMN missing_since TIMESTAMPTZ;
