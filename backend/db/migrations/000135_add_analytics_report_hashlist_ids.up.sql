-- Add hashlist_ids column to analytics_reports to track which hashlists were selected
ALTER TABLE analytics_reports ADD COLUMN hashlist_ids BIGINT[];

-- Comment for documentation
COMMENT ON COLUMN analytics_reports.hashlist_ids IS 'Selected hashlist IDs. NULL = legacy (date range fallback)';
