-- Deny-by-default: ensure all existing resources have at least owner + admin ACL entries
-- Notebooks: creator gets full access, org admin gets full access
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT n.org_id, 'notebook', n.id, 'user', n.created_by::text,
       ARRAY['view','run','edit','share','delete','create']
FROM notebooks n
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'notebook' AND ae.resource_id = n.id
    AND ae.subject_type = 'user' AND ae.subject_id = n.created_by::text
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT n.org_id, 'notebook', n.id, 'org_role', 'admin',
       ARRAY['view','run','edit','share','delete','create']
FROM notebooks n
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'notebook' AND ae.resource_id = n.id
    AND ae.subject_type = 'org_role' AND ae.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Connectors: creator gets full access, org admin gets full access
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT c.org_id, 'connector', c.id, 'user', c.created_by::text,
       ARRAY['view','use','edit','share','delete']
FROM connectors c
WHERE c.created_by IS NOT NULL
AND NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'connector' AND ae.resource_id = c.id
    AND ae.subject_type = 'user' AND ae.subject_id = c.created_by::text
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT c.org_id, 'connector', c.id, 'org_role', 'admin',
       ARRAY['view','use','edit','share','delete']
FROM connectors c
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'connector' AND ae.resource_id = c.id
    AND ae.subject_type = 'org_role' AND ae.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Folders (non-home only): creator gets full access, org admin gets full access
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT f.org_id, 'folder', f.id, 'user', f.created_by::text,
       ARRAY['view','create','edit','manage','share','delete']
FROM folders f
WHERE f.is_home = false
AND NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'folder' AND ae.resource_id = f.id
    AND ae.subject_type = 'user' AND ae.subject_id = f.created_by::text
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT f.org_id, 'folder', f.id, 'org_role', 'admin',
       ARRAY['view','create','edit','manage','share','delete']
FROM folders f
WHERE f.is_home = false
AND NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'folder' AND ae.resource_id = f.id
    AND ae.subject_type = 'org_role' AND ae.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Dashboards: creator gets full access, org admin gets full access
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT d.org_id, 'dashboard', d.id, 'user', d.created_by::text,
       ARRAY['view','run','edit','share','delete']
FROM dashboards d
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'dashboard' AND ae.resource_id = d.id
    AND ae.subject_type = 'user' AND ae.subject_id = d.created_by::text
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT d.org_id, 'dashboard', d.id, 'org_role', 'admin',
       ARRAY['view','run','edit','share','delete']
FROM dashboards d
WHERE NOT EXISTS (
    SELECT 1 FROM acl_entries ae
    WHERE ae.resource_type = 'dashboard' AND ae.resource_id = d.id
    AND ae.subject_type = 'org_role' AND ae.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;