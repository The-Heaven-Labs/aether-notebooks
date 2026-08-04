-- Migration 092: Add all_builtin_tools flag to agents
-- When true, the engine loads ALL built-in tools for the org (type='builtin')
-- instead of only the tools explicitly listed in tool_ids. This lets agents
-- automatically pick up new built-in tools without a per-agent migration.

ALTER TABLE agents
  ADD COLUMN all_builtin_tools bool NOT NULL DEFAULT false;
