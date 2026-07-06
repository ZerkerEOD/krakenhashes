package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TeamAgentTrust represents a directional trust relationship between teams.
// Row (TeamID=Audit, TrustedTeamID=IT) means "Audit trusts IT's agents to run Audit's jobs"
type TeamAgentTrust struct {
	TeamID        uuid.UUID  `json:"team_id"`
	TrustedTeamID uuid.UUID  `json:"trusted_team_id"`
	CreatedAt     time.Time  `json:"created_at"`
	CreatedBy     *uuid.UUID `json:"created_by,omitempty"`
}

// TeamNameOnly is a lightweight team reference for UI pickers
type TeamNameOnly struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	AgentCount int       `json:"agent_count"`
}

// Team represents a team in the system
type Team struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Users       []User    `json:"users,omitempty"`
	Agents      []Agent   `json:"agents,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ScanUsers scans a JSON-encoded users string into the Users slice
func (t *Team) ScanUsers(value interface{}) error {
	if value == nil {
		t.Users = []User{}
		return nil
	}

	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, &t.Users)
	case string:
		return json.Unmarshal([]byte(v), &t.Users)
	default:
		return fmt.Errorf("unsupported type for users: %T", value)
	}
}

// UsersValue returns the JSON encoding of Users for database storage
func (t Team) UsersValue() (driver.Value, error) {
	if len(t.Users) == 0 {
		return nil, nil
	}
	return json.Marshal(t.Users)
}

// ScanAgents scans a JSON-encoded agents string into the Agents slice
func (t *Team) ScanAgents(value interface{}) error {
	if value == nil {
		t.Agents = []Agent{}
		return nil
	}

	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, &t.Agents)
	case string:
		return json.Unmarshal([]byte(v), &t.Agents)
	default:
		return fmt.Errorf("unsupported type for agents: %T", value)
	}
}

// AgentsValue returns the JSON encoding of Agents for database storage
func (t Team) AgentsValue() (driver.Value, error) {
	if len(t.Agents) == 0 {
		return nil, nil
	}
	return json.Marshal(t.Agents)
}
