CREATE TABLE groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (org_id, name)
);

CREATE TABLE group_members (
  group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, user_id)
);

CREATE TABLE acl_entries (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  resource_type TEXT NOT NULL CHECK (resource_type IN ('folder','notebook','connector','dashboard')),
  resource_id   UUID NOT NULL,
  subject_type  TEXT NOT NULL CHECK (subject_type IN ('user','group','org_role')),
  subject_id    TEXT NOT NULL,
  actions       TEXT[] NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (resource_type, resource_id, subject_type, subject_id)
);

CREATE INDEX idx_acl_resource ON acl_entries (resource_type, resource_id);
CREATE INDEX idx_acl_subject  ON acl_entries (subject_type, subject_id);

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT f.org_id, 'folder', f.id, 'user', f.owner_id::text,
       ARRAY['view','create','edit','manage','delete']
FROM folders f WHERE f.is_home = true;
