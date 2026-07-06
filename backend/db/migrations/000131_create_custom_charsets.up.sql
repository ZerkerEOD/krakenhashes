-- Create custom_charsets table for saved/reusable charset definitions
-- Supports three scopes:
--   'global' (owner_id=NULL): Admin-created, visible everywhere
--   'user'   (owner_id=user UUID): Personal to that user
--   'team'   (owner_id=team UUID): Visible to team members

CREATE TABLE custom_charsets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    definition  TEXT NOT NULL,
    scope       TEXT NOT NULL DEFAULT 'global' CHECK (scope IN ('global', 'user', 'team')),
    owner_id    UUID DEFAULT NULL,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (name, scope, owner_id)
);

CREATE INDEX idx_custom_charsets_scope ON custom_charsets(scope);
CREATE INDEX idx_custom_charsets_scope_owner ON custom_charsets(scope, owner_id);
