-- Allow-only table grants, mirroring acl_entries semantics (no deny entries).
-- subject_type 'everyone' pairs with subject_id 'everyone'; 'user'/'group'
-- pair with the entity UUID as text. Rows (not arrays) so the sync worker can
-- diff desired vs actual with simple set operations.
CREATE TABLE warehouse_table_grants (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    warehouse_id  UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    subject_type  TEXT NOT NULL CHECK (subject_type IN ('user','group','everyone')),
    subject_id    TEXT NOT NULL,
    database_name TEXT NOT NULL,
    table_name    TEXT NOT NULL,
    created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (warehouse_id, subject_type, subject_id, database_name, table_name)
);

CREATE INDEX idx_wh_grants_subject ON warehouse_table_grants (warehouse_id, subject_type, subject_id);
CREATE INDEX idx_wh_grants_group ON warehouse_table_grants (subject_id) WHERE subject_type = 'group';
