-- Allow global agent sessions without a notebook context
ALTER TABLE agent_sessions ALTER COLUMN notebook_id DROP NOT NULL;
