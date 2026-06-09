-- Drop the CHECK constraint on connectors.type to allow dynamic connector types
-- managed by the driver registry instead of database-level enforcement.
ALTER TABLE connectors DROP CONSTRAINT IF EXISTS connectors_type_check;
