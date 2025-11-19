-- Create benchmark_requests table to track in-flight benchmarks
CREATE TABLE benchmark_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    job_execution_id UUID NOT NULL REFERENCES job_executions(id) ON DELETE CASCADE,
    attack_mode VARCHAR(50) NOT NULL,
    hash_type INT NOT NULL,
    request_type VARCHAR(20) NOT NULL CHECK (request_type IN ('forced', 'agent_speed')),
    requested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP,
    success BOOLEAN,
    error_message TEXT,
    UNIQUE(agent_id, attack_mode, hash_type)
);

-- Index for efficiently finding pending benchmarks
CREATE INDEX idx_benchmark_requests_pending
ON benchmark_requests(agent_id, completed_at)
WHERE completed_at IS NULL;

-- Index for cleanup queries
CREATE INDEX idx_benchmark_requests_cleanup
ON benchmark_requests(requested_at);

COMMENT ON TABLE benchmark_requests IS 'Tracks in-flight benchmark requests to enable parallel benchmarking with completion waiting';
COMMENT ON COLUMN benchmark_requests.request_type IS 'Type of benchmark: forced (for new jobs needing keyspace) or agent_speed (for agent performance data)';
COMMENT ON COLUMN benchmark_requests.completed_at IS 'Timestamp when benchmark completed; NULL means still pending';
