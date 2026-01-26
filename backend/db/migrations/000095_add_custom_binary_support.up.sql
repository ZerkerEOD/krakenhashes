-- Add source_type to track where binary came from (url download vs direct upload)
ALTER TABLE binary_versions ADD COLUMN source_type VARCHAR(20) NOT NULL DEFAULT 'url';
ALTER TABLE binary_versions ADD CONSTRAINT check_source_type CHECK (source_type IN ('url', 'upload'));

-- Add description for custom binaries documentation
ALTER TABLE binary_versions ADD COLUMN description TEXT;

-- Add explicit version field (can override auto-detected version from filename)
ALTER TABLE binary_versions ADD COLUMN version VARCHAR(100);

-- Make source_url nullable for uploaded binaries (they don't have a URL)
ALTER TABLE binary_versions ALTER COLUMN source_url DROP NOT NULL;

-- Index for filtering by source type
CREATE INDEX idx_binary_versions_source_type ON binary_versions(source_type);
