-- Migration 094: Revoke 'use' from 'everyone' ACL on built-in tools
--
-- The 'everyone' ACL entry with 'use' made checkToolUsePermission always
-- return true for built-in tools, rendering the ACL check ineffective.
--
-- Under the new model:
-- - tool_ids is the sole execution enforcement point (no ACL check at runtime)
-- - Tool ACLs control assignment (who can add a tool to an agent's tool_ids)
-- - 'everyone' gets 'view' only (UI listing); 'admin' gets full management

-- Remove 'use' from 'everyone' ACL entries on all tools
UPDATE acl_entries
SET actions = array_remove(actions, 'use')
WHERE resource_type = 'tool'
  AND subject_type = 'org_role'
  AND subject_id = 'everyone'
  AND 'use' = ANY(actions);

-- Add admin management ACL entries for all tools (if not exists)
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT t.org_id, 'tool', t.id, 'org_role', 'admin', ARRAY['view','use','edit','delete']
FROM tools t
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Clean up empty action arrays (if any)
DELETE FROM acl_entries
WHERE resource_type = 'tool'
  AND actions = '{}';
