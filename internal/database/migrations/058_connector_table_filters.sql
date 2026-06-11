-- Add table allowlist/denylist filters to connectors
-- These are regex patterns to filter which tables appear in the schema browser

ALTER TABLE connectors
    ADD COLUMN table_allowlist TEXT[] DEFAULT '{}',
    ADD COLUMN table_denylist TEXT[] DEFAULT '{}';

COMMENT ON COLUMN connectors.table_allowlist IS 'Regex patterns for tables to include in schema (empty means all)';
COMMENT ON COLUMN connectors.table_denylist IS 'Regex patterns for tables to exclude from schema';
