-- Warehouses group connectors that share one ClickHouse access namespace
-- (ClickHouse Cloud: services sharing a warehouse share users, passwords,
-- roles, and grants). A connector with NULL warehouse_id behaves as a
-- warehouse of one. A standalone connector row is promoted by inserting a
-- warehouse and pointing the connector at it.
CREATE TABLE warehouses (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                   UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name                     TEXT NOT NULL,
    -- The connector whose stored credential is used for provisioning DDL.
    -- Must be a read-write, non-idling service. SET NULL if deleted so the
    -- warehouse becomes invalid until an admin picks a new provisioner.
    provisioner_connector_id UUID REFERENCES connectors(id) ON DELETE SET NULL,
    sync_status              TEXT NOT NULL DEFAULT 'pending'
                             CHECK (sync_status IN ('pending','syncing','ready','error')),
    sync_error               TEXT,
    last_synced_at           TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

ALTER TABLE connectors ADD COLUMN warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL;
CREATE INDEX idx_connectors_warehouse ON connectors (warehouse_id);
CREATE INDEX idx_warehouses_org ON warehouses (org_id);
