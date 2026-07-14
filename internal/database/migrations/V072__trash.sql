ALTER TABLE notebooks ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE connectors ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE dashboards ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE folders ADD COLUMN deleted_at TIMESTAMPTZ;

CREATE INDEX idx_notebooks_deleted_at ON notebooks(deleted_at);
CREATE INDEX idx_connectors_deleted_at ON connectors(deleted_at);
CREATE INDEX idx_dashboards_deleted_at ON dashboards(deleted_at);
CREATE INDEX idx_folders_deleted_at ON folders(deleted_at);
