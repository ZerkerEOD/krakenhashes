package models

import "github.com/google/uuid"

// DefaultTeamID is the well-known UUID for the system Default Team
// This team is used for backwards compatibility and orphan handling
var DefaultTeamID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// DefaultTeamName is the name of the system Default Team
const DefaultTeamName = "Default Team"

// TeamRoles defines valid roles in the user_teams junction table
const (
	TeamRoleMember = "member" // Regular team member
	TeamRoleAdmin  = "admin"  // Team manager/administrator
)

// SystemSettingTeamsEnabled is the key for the teams feature toggle
const SystemSettingTeamsEnabled = "teams_enabled"
