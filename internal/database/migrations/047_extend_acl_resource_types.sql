-- Migration 047: Extend ACL resource types for agent system
-- Adds 'agent', 'model_config', 'skill' to the acl_entries resource_type constraint

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'acl_entries_resource_type_check') THEN
        ALTER TABLE acl_entries DROP CONSTRAINT acl_entries_resource_type_check;
    END IF;

    ALTER TABLE acl_entries ADD CONSTRAINT acl_entries_resource_type_check
        CHECK (resource_type IN ('folder', 'notebook', 'connector', 'dashboard', 'agent', 'model_config', 'skill'));
END $$;
