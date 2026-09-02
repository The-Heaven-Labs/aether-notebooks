-- Migration 093: Demote all_builtin_tools to UI-only helper.
-- The flag is no longer read by the engine (see V092). Tool access is governed
-- solely by tool_ids plus per-tool ACL entries.
-- Backfill tool_ids for agents that had all_builtin_tools=true so they do not
-- silently lose access, then reset the flag to false. Column stays for one
-- release as a no-op for backward compat with old clients (follow-up migration
-- will drop it).

-- Backfill: for each org, union existing tool_ids with all built-in tool ids
-- for agents that previously had all_builtin_tools=true.
WITH builtin AS (
    SELECT org_id, array_agg(id) AS ids FROM tools WHERE type = 'builtin' GROUP BY org_id
)
UPDATE agents a
SET tool_ids = (
    -- missing builtins || existing tool_ids
    SELECT array(
        SELECT t
        FROM unnest(builtin.ids) AS t
        WHERE NOT (t = ANY(COALESCE(a.tool_ids, '{}')))
    ) || COALESCE(a.tool_ids, '{}')
)
FROM builtin
WHERE a.org_id = builtin.org_id
  AND a.all_builtin_tools = true;

-- Reset the flag so it reflects reality (always false after this migration).
UPDATE agents SET all_builtin_tools = false WHERE all_builtin_tools = true;
