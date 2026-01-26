package services

import (
	"context"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// AgentCleanupService handles agent status cleanup on startup
type AgentCleanupService struct {
	agentRepo *repository.AgentRepository
}

// NewAgentCleanupService creates a new agent cleanup service
func NewAgentCleanupService(agentRepo *repository.AgentRepository) *AgentCleanupService {
	return &AgentCleanupService{
		agentRepo: agentRepo,
	}
}

// MarkAllAgentsInactive marks all agents as inactive on startup
func (s *AgentCleanupService) MarkAllAgentsInactive(ctx context.Context) error {
	debug.Log("Marking all agents as inactive on startup", nil)

	// Get all agents
	agents, err := s.agentRepo.List(ctx, nil)
	if err != nil {
		return err
	}

	// Mark each agent as inactive
	for _, agent := range agents {
		if agent.Status == models.AgentStatusActive {
			debug.Log("Marking agent as inactive on startup", map[string]interface{}{
				"agent_id":        agent.ID,
				"previous_status": agent.Status,
			})

			err := s.agentRepo.UpdateStatus(ctx, agent.ID, models.AgentStatusInactive, nil)
			if err != nil {
				debug.Error("Failed to mark agent %d as inactive: %v", agent.ID, err)
				// Continue with other agents
			}
		}
	}

	debug.Log("Completed agent cleanup", map[string]interface{}{
		"total_agents": len(agents),
	})

	return nil
}

// CleanupStaleAgents marks agents as inactive if they haven't sent heartbeat recently
func (s *AgentCleanupService) CleanupStaleAgents(ctx context.Context, heartbeatTimeout time.Duration) error {
	agents, err := s.agentRepo.List(ctx, map[string]interface{}{"status": models.AgentStatusActive})
	if err != nil {
		return err
	}

	for _, agent := range agents {
		// Check if heartbeat is stale
		if agent.LastHeartbeat.IsZero() || time.Since(agent.LastHeartbeat) > heartbeatTimeout {
			debug.Log("Marking stale agent as inactive", map[string]interface{}{
				"agent_id":       agent.ID,
				"last_heartbeat": agent.LastHeartbeat,
				"timeout":        heartbeatTimeout,
			})

			err := s.agentRepo.UpdateStatus(ctx, agent.ID, models.AgentStatusInactive, nil)
			if err != nil {
				debug.Error("Failed to mark stale agent %d as inactive: %v", agent.ID, err)
			}
		}
	}

	return nil
}
