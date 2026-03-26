-- New display state columns on cells
ALTER TABLE cells ADD COLUMN IF NOT EXISTS source_visible BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE cells ADD COLUMN IF NOT EXISTS cell_collapsed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE cells ADD COLUMN IF NOT EXISTS title VARCHAR(255);
ALTER TABLE cells ADD COLUMN IF NOT EXISTS description TEXT;
ALTER TABLE cells ADD COLUMN IF NOT EXISTS slug VARCHAR(100);

-- Slug uniqueness per notebook (only when set)
CREATE UNIQUE INDEX IF NOT EXISTS idx_cells_notebook_slug
  ON cells (notebook_id, slug)
  WHERE slug IS NOT NULL;

-- Per-cell version history
CREATE TABLE IF NOT EXISTS cell_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  cell_id UUID NOT NULL REFERENCES cells(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cell_versions_cell_created
  ON cell_versions (cell_id, created_at DESC);

-- Notebook-level snapshots
CREATE TABLE IF NOT EXISTS notebook_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  notebook_id UUID NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  cell_sources JSONB NOT NULL,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notebook_snapshots_nb
  ON notebook_snapshots (notebook_id, created_at DESC);
