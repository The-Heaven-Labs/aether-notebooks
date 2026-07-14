CREATE TABLE folders (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  parent_id UUID REFERENCES folders(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  is_home BOOLEAN NOT NULL DEFAULT false,
  owner_id UUID REFERENCES users(id) ON DELETE CASCADE,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (org_id, parent_id, name)
);

CREATE INDEX idx_folders_org ON folders (org_id);
CREATE INDEX idx_folders_parent ON folders (parent_id);

ALTER TABLE notebooks  ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;
ALTER TABLE connectors ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;
ALTER TABLE dashboards ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;

INSERT INTO folders (org_id, name, is_home, owner_id, created_by)
SELECT om.org_id, u.name || '''s Home', true, u.id, u.id
FROM users u
JOIN org_members om ON om.user_id = u.id;
