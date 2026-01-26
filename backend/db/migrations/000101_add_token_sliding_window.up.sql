-- Add columns for token sliding window with grace period
-- Allows old tokens to remain valid briefly after being superseded

ALTER TABLE tokens ADD COLUMN superseded_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE tokens ADD COLUMN superseded_by UUID REFERENCES tokens(id) ON DELETE SET NULL;

-- Index for efficient cleanup of expired superseded tokens
CREATE INDEX idx_tokens_superseded_at ON tokens(superseded_at) WHERE superseded_at IS NOT NULL;

COMMENT ON COLUMN tokens.superseded_at IS 'Timestamp when this token was superseded by a new token. Token remains valid for grace period after this.';
COMMENT ON COLUMN tokens.superseded_by IS 'Reference to the new token that superseded this one.';
