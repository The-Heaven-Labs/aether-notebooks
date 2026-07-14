ALTER TABLE connectors ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX IF NOT EXISTS idx_connectors_one_default_per_org
  ON connectors (org_id)
  WHERE is_default = true;
