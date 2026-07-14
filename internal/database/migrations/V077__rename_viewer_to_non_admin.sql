ALTER TABLE org_members DROP CONSTRAINT org_members_role_check;

UPDATE org_members SET role = 'non-admin' WHERE role = 'viewer';

ALTER TABLE org_members ADD CONSTRAINT org_members_role_check
  CHECK (role = ANY (ARRAY['admin'::text, 'editor'::text, 'non-admin'::text]));
