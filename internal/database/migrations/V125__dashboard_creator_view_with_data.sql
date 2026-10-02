-- Migration 125: backfill view_with_data on dashboard creator ACL entries.
--
-- V121 introduced per-dashboard `view_with_data` and made dashboard creation
-- seed it for the creator. Dashboards created before that kept the old action
-- list {view,edit,delete,share}, so their creators could not run query widgets:
-- POST /dashboards/{id}/execute requires view_with_data and admin mode is not
-- part of the normal flow. New dashboards are unaffected; this backfill only
-- appends the action to existing creator entries, leaving every other subject's
-- data access untouched.

-- Markers let the migration test execute exactly this statement against
-- pre-migration ACL rows.
-- +backfill:start
UPDATE acl_entries a
SET actions = array_append(a.actions, 'view_with_data')
FROM dashboards d
WHERE a.resource_type = 'dashboard'
  AND a.resource_id = d.id
  AND a.org_id = d.org_id
  AND a.subject_type = 'user'
  AND a.subject_id = d.created_by::text
  AND NOT a.actions @> ARRAY['view_with_data'];
-- +backfill:end
