-- Per-session auto-approve flag so headless/API clients have ConfirmRequired
-- tools resolved by the engine without waiting for client input.
ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS auto_approve_tools BOOLEAN NOT NULL DEFAULT FALSE;
