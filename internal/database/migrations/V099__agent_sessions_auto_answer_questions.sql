-- Per-session auto-answer flag so headless/API clients have ask_question
-- resolved by the engine instead of blocking until the context deadline.
-- Independent of auto_approve_tools: a session may auto-approve tools while
-- still wanting questions to surface, or vice versa.
ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS auto_answer_questions BOOLEAN NOT NULL DEFAULT FALSE;
