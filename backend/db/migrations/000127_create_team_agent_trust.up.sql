-- Team agent trust: directional trust relationships between teams
-- Row (team_id=Audit, trusted_team_id=IT) means "Audit trusts IT's agents to run Audit's jobs"
CREATE TABLE IF NOT EXISTS team_agent_trust (
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    trusted_team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_by UUID REFERENCES users(id),
    PRIMARY KEY (team_id, trusted_team_id),
    CHECK (team_id != trusted_team_id)
);

CREATE INDEX IF NOT EXISTS idx_team_agent_trust_team ON team_agent_trust(team_id);
CREATE INDEX IF NOT EXISTS idx_team_agent_trust_trusted ON team_agent_trust(trusted_team_id);
