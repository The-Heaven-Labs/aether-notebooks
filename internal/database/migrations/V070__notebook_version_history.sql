-- Enrich notebook_snapshots for full notebook version history (Google Docs style)
ALTER TABLE notebook_snapshots ADD COLUMN IF NOT EXISTS title TEXT;
ALTER TABLE notebook_snapshots ADD COLUMN IF NOT EXISTS cells JSONB;
ALTER TABLE notebook_snapshots ADD COLUMN IF NOT EXISTS auto BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE notebook_snapshots ADD COLUMN IF NOT EXISTS created_by_name TEXT;

CREATE INDEX IF NOT EXISTS idx_notebook_snapshots_auto
  ON notebook_snapshots (notebook_id, created_at DESC)
  WHERE auto = true;
