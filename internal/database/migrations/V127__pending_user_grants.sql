-- Pending-user grants: stage ACL entries and warehouse table grants by email
-- before the person has an account, mirroring pending_group_members (V105).
-- Rows are materialized into acl_entries / warehouse_table_grants and consumed
-- when the email first joins the org (registration, org join, SSO
-- provisioning/auto-join, invite redemption).
--
-- Staged rows carry no FK to users (the row exists precisely while the email
-- has no account) and are keyed by lower(email) so lookups are
-- case-insensitive. resource_id is polymorphic, so it carries no FK either;
-- trash purge cleans it up explicitly (internal/scheduler/scheduler.go).
CREATE TABLE pending_acl_entries (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('folder','notebook','connector','dashboard',
                       'agent','model_config','skill','mcp_server','tool','agent_session')),
    resource_id   UUID NOT NULL,
    email         TEXT NOT NULL,
    actions       TEXT[] NOT NULL,
    created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Expression unique constraints are not valid in a table constraint, so this
-- is a unique index instead. Also makes
-- ON CONFLICT (resource_type, resource_id, lower(email)) inference work.
CREATE UNIQUE INDEX uq_pending_acl_resource_email
    ON pending_acl_entries (resource_type, resource_id, lower(email));
CREATE INDEX idx_pending_acl_org_email ON pending_acl_entries (org_id, lower(email));

CREATE TABLE pending_warehouse_table_grants (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    warehouse_id  UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    email         TEXT NOT NULL,
    database_name TEXT NOT NULL,
    table_name    TEXT NOT NULL,
    created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Also makes ON CONFLICT (warehouse_id, lower(email), database_name,
-- table_name) inference work.
CREATE UNIQUE INDEX uq_pending_wh_grants
    ON pending_warehouse_table_grants (warehouse_id, lower(email), database_name, table_name);
CREATE INDEX idx_pending_wh_grants_org_email ON pending_warehouse_table_grants (org_id, lower(email));
