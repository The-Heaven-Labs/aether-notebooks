-- Published dashboard snapshots for the draft → publish split. Each row is an
-- immutable snapshot of the dashboard document's content (title, settings with
-- variables folded in as settings["variables"], and full widget definitions)
-- taken at publish time; restore copies a snapshot back into the draft.
-- No separate index: UNIQUE (dashboard_id, version) already serves the
-- per-dashboard newest-first scans.
CREATE TABLE IF NOT EXISTS dashboard_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    version INT NOT NULL,
    title TEXT NOT NULL,
    settings JSONB NOT NULL,
    widgets JSONB NOT NULL,
    content_hash TEXT NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (dashboard_id, version)
);
