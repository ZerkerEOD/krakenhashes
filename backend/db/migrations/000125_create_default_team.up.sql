-- Migration: Create the Default Team
-- Purpose: Provides a fallback team for backwards compatibility and orphan handling
-- The Default Team has a well-known UUID for programmatic reference

-- Use a specific UUID so it can be referenced in code
-- UUID: 00000000-0000-0000-0000-000000000001
INSERT INTO teams (id, name, description, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'Default Team',
    'System default team for backwards compatibility. Orphaned resources are reassigned here when their team is deleted. Only administrators have access to this team by default.',
    NOW(),
    NOW()
)
ON CONFLICT (id) DO UPDATE SET
    description = EXCLUDED.description,
    updated_at = NOW();

-- Note: We do NOT automatically add users to the Default Team
-- This ensures maximum isolation - only admins can see orphaned resources
-- Admins can manually add users to Default Team if needed

-- Add a constraint to prevent deletion of the Default Team
-- This is enforced at the application level, but we add a trigger as backup

CREATE OR REPLACE FUNCTION prevent_default_team_deletion()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.id = '00000000-0000-0000-0000-000000000001' THEN
        RAISE EXCEPTION 'Cannot delete the Default Team (ID: 00000000-0000-0000-0000-000000000001). This team is required for system operation.';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

-- Create the trigger
DROP TRIGGER IF EXISTS tr_prevent_default_team_deletion ON teams;
CREATE TRIGGER tr_prevent_default_team_deletion
    BEFORE DELETE ON teams
    FOR EACH ROW
    EXECUTE FUNCTION prevent_default_team_deletion();

-- Add comment
COMMENT ON FUNCTION prevent_default_team_deletion() IS 'Prevents deletion of the system Default Team';
