CREATE INDEX IF NOT EXISTS idx_notebooks_folder ON notebooks (folder_id);
CREATE INDEX IF NOT EXISTS idx_connectors_folder ON connectors (folder_id);
CREATE INDEX IF NOT EXISTS idx_dashboards_folder ON dashboards (folder_id);
