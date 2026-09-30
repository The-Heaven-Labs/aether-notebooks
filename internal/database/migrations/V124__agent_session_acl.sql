-- Migration 124: agent_session ACL type, notebook-viewer inheritance flag,
-- owner ACL backfill, and the notebook session listing index.

-- Extend the ACL CHECK constraint to include agent_session.
ALTER TABLE acl_entries DROP CONSTRAINT IF EXISTS acl_entries_resource_type_check;
ALTER TABLE acl_entries ADD CONSTRAINT acl_entries_resource_type_check
    CHECK (resource_type IN ('folder','notebook','connector','dashboard','agent',
                             'model_config','skill','mcp_server','tool','agent_session'));

-- Opt-in: when true and the session has a notebook, notebook viewers may read it.
ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS share_with_notebook_viewers BOOLEAN NOT NULL DEFAULT FALSE;

-- Backfill owner ACL entries for existing sessions. Markers let the migration
-- test execute exactly this statement against pre-migration session rows.
-- +backfill:start
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT a.org_id, 'agent_session', s.id, 'user', s.user_id::text,
       ARRAY['view','edit','share','delete','admin']
FROM agent_sessions s
JOIN agents a ON a.id = s.agent_id
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;
-- +backfill:end

CREATE INDEX IF NOT EXISTS idx_agent_sessions_notebook
    ON agent_sessions (notebook_id, created_at DESC);
