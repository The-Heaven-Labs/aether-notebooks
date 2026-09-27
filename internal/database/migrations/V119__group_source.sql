-- Tag each group with how it came to exist.
ALTER TABLE groups ADD COLUMN source text NOT NULL DEFAULT 'manual';

ALTER TABLE groups ADD CONSTRAINT groups_source_check
  CHECK (source IN ('manual', 'sso', 'system'));

UPDATE groups SET source = 'system' WHERE LOWER(name) = 'everyone';

UPDATE groups SET source = 'sso'
 WHERE source = 'manual'
   AND id IN (SELECT DISTINCT group_id FROM sso_group_memberships);
