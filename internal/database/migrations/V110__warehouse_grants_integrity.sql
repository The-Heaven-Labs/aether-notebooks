-- Close the org/warehouse integrity gap: a grant's org_id must match the
-- warehouse's org. Composite FK instead of the single-column one. Also drop
-- the redundant index (prefix of the unique btree) in favour of a
-- subject-first lookup index, and enforce subject_id canonical form.
ALTER TABLE warehouses ADD CONSTRAINT warehouses_id_org_key UNIQUE (id, org_id);

ALTER TABLE warehouse_table_grants DROP CONSTRAINT warehouse_table_grants_warehouse_id_fkey;
ALTER TABLE warehouse_table_grants
    ADD CONSTRAINT warehouse_table_grants_warehouse_org_fkey
    FOREIGN KEY (warehouse_id, org_id) REFERENCES warehouses(id, org_id) ON DELETE CASCADE;

DROP INDEX idx_wh_grants_subject;
DROP INDEX idx_wh_grants_group;
CREATE INDEX idx_wh_grants_subject_lookup ON warehouse_table_grants (subject_type, subject_id);

ALTER TABLE warehouse_table_grants
    ADD CONSTRAINT warehouse_table_grants_subject_canonical CHECK (
        (subject_type = 'everyone' AND subject_id = 'everyone')
        OR (subject_type IN ('user','group')
            AND subject_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
    );
