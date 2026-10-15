-- Migration 135: listing performance (org+folder+sort, alive rows) and
-- content-search GIN indexes. The listing indexes supersede the org-only
-- single-column scans for alive-row listing queries with composite indexes
-- ordered for the canonical "WHERE org_id = ? AND folder_id = ?
-- AND deleted_at IS NULL ORDER BY updated_at DESC" folder-listing queries;
-- the existing single-column indexes stay load-bearing for trashed-row
-- listings and FK cascades, so do not drop them later.
--
-- folders gets no listing index on purpose: V009's UNIQUE (org_id,
-- parent_id, name) already serves the folder-children scan, and its leading
-- columns satisfy ORDER BY name without a sort.
--
-- The two GIN indexes back core-Postgres FTS (to_tsvector('simple', …)) for
-- the planned content search; the two-argument to_tsvector form is immutable,
-- so it can be indexed, and the explicit 'simple' config keeps stemming out
-- of the picture (substring/prefix-friendly, no extension required).
--
-- Large-instance deploy note: every non-concurrent CREATE INDEX blocks writes
-- on its table (SHARE lock) for the duration of the build, and the two GIN
-- builds are the expensive ones. Operators may pre-create any of the five
-- indexes with CREATE INDEX CONCURRENTLY using these exact names and
-- definitions — the IF NOT EXISTS clauses make this migration a no-op
-- afterwards.
CREATE INDEX IF NOT EXISTS idx_notebooks_org_folder_updated
  ON notebooks (org_id, folder_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_dashboards_org_folder_updated
  ON dashboards (org_id, folder_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_connectors_org_folder
  ON connectors (org_id, folder_id) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_cells_source_fts
  ON cells USING gin (to_tsvector('simple', source));
CREATE INDEX IF NOT EXISTS idx_widgets_query_fts
  ON widgets USING gin (to_tsvector('simple', query));
