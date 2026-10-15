-- Migration 135: listing performance (org+folder+sort, alive rows) and
-- content-search GIN indexes. The listing indexes replace the org-only
-- btree scans with composite indexes ordered for the canonical
-- "WHERE org_id = ? AND folder_id = ? AND deleted_at IS NULL ORDER BY
-- updated_at DESC" folder-listing queries. The two GIN indexes back core
-- Postgres FTS (to_tsvector('simple', …)) used by the content search in
-- internal/search; the two-argument to_tsvector form is immutable, so it
-- can be indexed, and the explicit 'simple' config keeps stemming out of
-- the picture (substring/prefix-friendly, no extension required).
CREATE INDEX IF NOT EXISTS idx_notebooks_org_folder_updated
  ON notebooks (org_id, folder_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_dashboards_org_folder_updated
  ON dashboards (org_id, folder_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_connectors_org_folder
  ON connectors (org_id, folder_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_folders_org_parent
  ON folders (org_id, parent_id) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_cells_source_fts
  ON cells USING gin (to_tsvector('simple', source));
CREATE INDEX IF NOT EXISTS idx_widgets_query_fts
  ON widgets USING gin (to_tsvector('simple', query));
