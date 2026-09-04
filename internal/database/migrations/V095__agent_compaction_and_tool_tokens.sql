-- Migration 095: Tool direct tokens + compaction divider
-- Issue 1: per-tool token attribution
-- Issue 2: compaction visibility + trigger bug fix
ALTER TABLE agent_messages ADD COLUMN IF NOT EXISTS tokens_direct INT NOT NULL DEFAULT 0;

-- Extend role CHECK to allow compaction divider rows
-- Constraint name is agent_messages_role_check (from V046); drop if exists then recreate
ALTER TABLE agent_messages DROP CONSTRAINT IF EXISTS agent_messages_role_check;
ALTER TABLE agent_messages ADD CONSTRAINT agent_messages_role_check
    CHECK (role IN ('user', 'assistant', 'tool', 'compaction', 'subagent'));
