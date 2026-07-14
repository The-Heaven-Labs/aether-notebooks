-- Migration 062: Add mcp_server to ACL resource types + backfill ACL entries

-- Extend the CHECK constraint to include mcp_server
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'acl_entries_resource_type_check') THEN
        ALTER TABLE acl_entries DROP CONSTRAINT acl_entries_resource_type_check;
    END IF;

    ALTER TABLE acl_entries ADD CONSTRAINT acl_entries_resource_type_check
        CHECK (resource_type IN ('folder', 'notebook', 'connector', 'dashboard', 'agent', 'model_config', 'skill', 'mcp_server'));
END $$;

-- Backfill ACL entries for agents
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT a.org_id, 'agent', a.id, 'user', a.created_by::text,
       ARRAY['view','edit','delete']
FROM agents a
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'agent' AND ae.resource_id = a.id
    AND ae.subject_type = 'user' AND ae.subject_id = a.created_by::text
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT a.org_id, 'agent', a.id, 'org_role', 'admin',
       ARRAY['view','edit','delete']
FROM agents a
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'agent' AND ae.resource_id = a.id
    AND ae.subject_type = 'org_role' AND ae.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Backfill ACL entries for model_configs
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT m.org_id, 'model_config', m.id, 'user', m.created_by::text,
       ARRAY['view','edit','delete']
FROM model_configs m
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'model_config' AND ae.resource_id = m.id
    AND ae.subject_type = 'user' AND ae.subject_id = m.created_by::text
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT m.org_id, 'model_config', m.id, 'org_role', 'admin',
       ARRAY['view','edit','delete']
FROM model_configs m
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'model_config' AND ae.resource_id = m.id
    AND ae.subject_type = 'org_role' AND ae.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Backfill ACL entries for skills
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT s.org_id, 'skill', s.id, 'user', s.created_by::text,
       ARRAY['view','edit','delete']
FROM skills s
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'skill' AND ae.resource_id = s.id
    AND ae.subject_type = 'user' AND ae.subject_id = s.created_by::text
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT s.org_id, 'skill', s.id, 'org_role', 'admin',
       ARRAY['view','edit','delete']
FROM skills s
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'skill' AND ae.resource_id = s.id
    AND ae.subject_type = 'org_role' AND ae.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Backfill ACL entries for mcp_servers
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT m.org_id, 'mcp_server', m.id, 'user', m.created_by::text,
       ARRAY['view','edit','delete']
FROM mcp_servers m
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'mcp_server' AND ae.resource_id = m.id
    AND ae.subject_type = 'user' AND ae.subject_id = m.created_by::text
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT m.org_id, 'mcp_server', m.id, 'org_role', 'admin',
       ARRAY['view','edit','delete']
FROM mcp_servers m
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'mcp_server' AND ae.resource_id = m.id
    AND ae.subject_type = 'org_role' AND ae.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;
