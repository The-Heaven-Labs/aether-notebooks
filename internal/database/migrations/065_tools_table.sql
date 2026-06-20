-- Migration 065: Tools table — reify tools as first-class resources
-- Creates the tools table, adds tool_ids to agents, drops tool_ids from skills,
-- and adds 'tool' to the ACL resource_type constraint.

CREATE TABLE tools (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL CHECK (type IN ('builtin', 'webhook', 'sql_query')),
    schema      JSONB NOT NULL DEFAULT '{}',
    config      JSONB NOT NULL DEFAULT '{}',
    folder_id   UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, name)
);

CREATE INDEX idx_tools_org ON tools (org_id);
CREATE INDEX idx_tools_folder ON tools (folder_id);

-- Add tool_ids to agents
ALTER TABLE agents ADD COLUMN tool_ids UUID[] NOT NULL DEFAULT '{}';

-- Drop tool_ids from skills (moved to separate tools table)
ALTER TABLE skills DROP COLUMN IF EXISTS tool_ids;

-- Extend the ACL CHECK constraint to include 'tool'
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'acl_entries_resource_type_check') THEN
        ALTER TABLE acl_entries DROP CONSTRAINT acl_entries_resource_type_check;
    END IF;

    ALTER TABLE acl_entries ADD CONSTRAINT acl_entries_resource_type_check
        CHECK (resource_type IN ('folder', 'notebook', 'connector', 'dashboard', 'agent', 'model_config', 'skill', 'mcp_server', 'tool'));
END $$;
