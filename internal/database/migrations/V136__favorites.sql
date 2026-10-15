-- Migration 136: per-user favorites for catalog resources.
--
-- resource_id is polymorphic (folder, notebook, dashboard, connector, agent,
-- model_config, skill, mcp_server, tool) and deliberately carries no FK — the
-- same convention as acl_entries — so cleanup is explicit in purge/hard-delete
-- paths (deleteFavoritesFor); user and org deletion cascade via the two FKs.
-- resource_type has no DB-level CHECK: the nine-type whitelist is enforced in
-- the API layer so adding a resource type needs no migration rewrite.
--
-- The UNIQUE key makes PUT /favorites/{type}/{id} an idempotent upsert, and
-- idx_favorites_user_org serves the /favorites listing scan (newest first).
-- No index on (resource_type, resource_id): per-user favorite volume keeps
-- the purge-time scans cheap at the scales this table sees.
CREATE TABLE favorites (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  resource_type TEXT NOT NULL,
  resource_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, org_id, resource_type, resource_id)
);
CREATE INDEX idx_favorites_user_org ON favorites (user_id, org_id, created_at DESC);
