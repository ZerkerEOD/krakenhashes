-- Create agent_benchmark_history table for historical benchmark tracking
-- This is an append-only table that preserves all benchmark results,
-- unlike agent_benchmarks which only keeps the latest per combination.
CREATE TABLE agent_benchmark_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    attack_mode INTEGER NOT NULL,
    hash_type INTEGER NOT NULL,
    salt_count INTEGER,
    speed BIGINT NOT NULL,
    success BOOLEAN NOT NULL DEFAULT true,
    error_message TEXT,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Primary lookup: agent history per hash type, newest first
CREATE INDEX idx_bench_hist_agent_lookup
ON agent_benchmark_history(agent_id, attack_mode, hash_type, recorded_at DESC);

-- Cleanup: time-based retention
CREATE INDEX idx_bench_hist_cleanup
ON agent_benchmark_history(recorded_at);

-- System setting for benchmark history retention
INSERT INTO system_settings (key, value, description, data_type)
VALUES ('benchmark_history_retention_days', '365', 'Days to retain benchmark history records (0 = unlimited)', 'integer')
ON CONFLICT (key) DO NOTHING;
