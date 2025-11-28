//go:build unit
// +build unit

package services

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockJobWebSocketIntegration is a mock implementation of JobWebSocketIntegration
type MockJobWebSocketIntegration struct {
	mock.Mock
}

func (m *MockJobWebSocketIntegration) SendJobAssignment(ctx context.Context, task *models.JobTask, jobExecution *models.JobExecution) error {
	args := m.Called(ctx, task, jobExecution)
	return args.Error(0)
}

func (m *MockJobWebSocketIntegration) RequestAgentBenchmark(ctx context.Context, agentID int, jobExecution *models.JobExecution, layerID *uuid.UUID, layerMask string) error {
	args := m.Called(ctx, agentID, jobExecution, layerID, layerMask)
	return args.Error(0)
}

// TestBenchmarkRequestFlow demonstrates the benchmark request flow
func TestBenchmarkRequestFlow(t *testing.T) {
	// This test demonstrates how the benchmark request flow works
	// when an agent needs a benchmark before receiving work

	ctx := context.Background()
	mockWS := new(MockJobWebSocketIntegration)

	// Test data
	agentID := 1
	presetJobID := uuid.New()
	jobExecution := &models.JobExecution{
		ID:          uuid.New(),
		PresetJobID: &presetJobID,
		HashlistID:  100,
		AttackMode:  models.AttackModeStraight,
	}

	// Set up expectation for benchmark request (no layer, so nil and "")
	mockWS.On("RequestAgentBenchmark", ctx, agentID, jobExecution, (*uuid.UUID)(nil), "").Return(nil)

	// Call the method
	err := mockWS.RequestAgentBenchmark(ctx, agentID, jobExecution, nil, "")

	// Verify
	assert.NoError(t, err)
	mockWS.AssertExpectations(t)
}

// TestJobAssignmentWithBenchmark demonstrates job assignment after benchmark
func TestJobAssignmentWithBenchmark(t *testing.T) {
	// This test demonstrates how job assignment works
	// after a valid benchmark is available

	ctx := context.Background()
	mockWS := new(MockJobWebSocketIntegration)

	// Test data
	agentID := 1
	presetJobID := uuid.New()
	task := &models.JobTask{
		ID:             uuid.New(),
		JobExecutionID: uuid.New(),
		AgentID:        &agentID,
		KeyspaceStart:  0,
		KeyspaceEnd:    1000000,
		Status:         models.JobTaskStatusAssigned,
	}

	jobExecution := &models.JobExecution{
		ID:          task.JobExecutionID,
		PresetJobID: &presetJobID,
		HashlistID:  100,
		AttackMode:  models.AttackModeStraight,
	}

	// Set up expectation for job assignment
	mockWS.On("SendJobAssignment", ctx, task, jobExecution).Return(nil)

	// Call the method
	err := mockWS.SendJobAssignment(ctx, task, jobExecution)

	// Verify
	assert.NoError(t, err)
	mockWS.AssertExpectations(t)
}
