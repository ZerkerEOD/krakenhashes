package interfaces

import (
	"context"
	"encoding/json"
)

// JobHandler handles job-related messages
type JobHandler interface {
	ProcessJobProgress(ctx context.Context, agentID int, payload json.RawMessage) error
	ProcessCrackBatch(ctx context.Context, agentID int, payload json.RawMessage) error
	ProcessBenchmarkResult(ctx context.Context, agentID int, payload json.RawMessage) error
}
