ALTER TABLE mcp_servers ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;

CREATE INDEX idx_mcp_servers_folder ON mcp_servers (folder_id);
