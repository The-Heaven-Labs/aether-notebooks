-- RLS was enabled on these tables in V001 but no policies were ever created,
-- making every non-owner role default-deny (silent zero-row reads, UPDATE 0).
-- Tenant isolation is enforced at the application layer (org_id scoping +
-- acl_entries checks); see the RFC for real DB-level policies.
ALTER TABLE orgs        DISABLE ROW LEVEL SECURITY;
ALTER TABLE notebooks   DISABLE ROW LEVEL SECURITY;
ALTER TABLE cells       DISABLE ROW LEVEL SECURITY;
ALTER TABLE connectors  DISABLE ROW LEVEL SECURITY;
ALTER TABLE dashboards  DISABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs  DISABLE ROW LEVEL SECURITY;