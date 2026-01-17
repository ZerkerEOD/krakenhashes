package jobs

import (
	"sync"
	"time"
)

// TaskState represents the explicit state of a task in the agent
type TaskState int

const (
	// TaskStateIdle indicates no task is running, agent is ready for work
	TaskStateIdle TaskState = iota
	// TaskStateRunning indicates a task is actively executing
	TaskStateRunning
	// TaskStateCompleting indicates task finished, awaiting backend ACK
	TaskStateCompleting
	// TaskStateFailed indicates task execution failed
	TaskStateFailed
	// TaskStateStopped indicates task was stopped by request
	TaskStateStopped
)

// String returns a human-readable representation of the task state
func (s TaskState) String() string {
	switch s {
	case TaskStateIdle:
		return "idle"
	case TaskStateRunning:
		return "running"
	case TaskStateCompleting:
		return "completing"
	case TaskStateFailed:
		return "failed"
	case TaskStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// CanAcceptNewTask returns true if the agent can accept a new task in this state
func (s TaskState) CanAcceptNewTask() bool {
	return s == TaskStateIdle
}

// IsTerminal returns true if this is a terminal state (task finished)
func (s TaskState) IsTerminal() bool {
	return s == TaskStateIdle || s == TaskStateFailed || s == TaskStateStopped
}

// TaskStateManager manages explicit task state transitions
// This replaces relying on activeJobs map membership for state tracking
type TaskStateManager struct {
	mu             sync.RWMutex
	currentState   TaskState
	currentTaskID  string
	stateChangedAt time.Time

	// Completion pending flag - set when ACK not received after retries
	// Will be resolved on next backend communication (state sync, reconnect)
	completionPending   bool
	completionPendingID string
}

// NewTaskStateManager creates a new task state manager
func NewTaskStateManager() *TaskStateManager {
	return &TaskStateManager{
		currentState:   TaskStateIdle,
		stateChangedAt: time.Now(),
	}
}

// TransitionTo atomically changes the task state
func (m *TaskStateManager) TransitionTo(newState TaskState, taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentState = newState
	m.currentTaskID = taskID
	m.stateChangedAt = time.Now()
}

// GetState returns the current state and task ID atomically
func (m *TaskStateManager) GetState() (TaskState, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentState, m.currentTaskID
}

// GetStateInfo returns full state information including timing
func (m *TaskStateManager) GetStateInfo() (TaskState, string, time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentState, m.currentTaskID, m.stateChangedAt
}

// SetCompletionPending marks a task completion as pending ACK resolution
func (m *TaskStateManager) SetCompletionPending(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completionPending = true
	m.completionPendingID = taskID
}

// ClearCompletionPending clears the pending completion flag
func (m *TaskStateManager) ClearCompletionPending() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completionPending = false
	m.completionPendingID = ""
}

// GetCompletionPending returns the pending completion status
func (m *TaskStateManager) GetCompletionPending() (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.completionPending, m.completionPendingID
}

// TimeSinceStateChange returns how long the agent has been in the current state
func (m *TaskStateManager) TimeSinceStateChange() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Since(m.stateChangedAt)
}
