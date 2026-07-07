ALTER TABLE agent_messages DROP CONSTRAINT IF EXISTS agent_messages_role_check;
ALTER TABLE agent_messages ADD CONSTRAINT agent_messages_role_check CHECK (role IN ('user', 'assistant', 'tool', 'subagent'));
