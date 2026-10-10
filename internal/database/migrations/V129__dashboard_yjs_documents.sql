-- Yjs document state for live dashboard co-editing (binary CRDT state).
CREATE TABLE dashboard_yjs_documents (
    dashboard_id UUID PRIMARY KEY REFERENCES dashboards(id) ON DELETE CASCADE,
    state BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
