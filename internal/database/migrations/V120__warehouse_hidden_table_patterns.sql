-- Warehouse-level regex patterns that hide matching tables from the grant
-- picker, the schema browser, and the new-tables inbox. Curation only: no
-- ClickHouse DDL or warehouse_table_grants rows are affected.
ALTER TABLE warehouses ADD COLUMN hidden_table_patterns TEXT[] NOT NULL DEFAULT '{}';
