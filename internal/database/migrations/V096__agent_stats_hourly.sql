-- Hourly agent usage rollup (finer grain than agent_stats_daily, includes
-- subagent/direct tokens, model calls, duration, and estimated cost).
-- Buckets accrue from deploy onward; agent_stats_daily is kept for history.
CREATE TABLE IF NOT EXISTS agent_stats_hourly (
    bucket_start      TIMESTAMPTZ NOT NULL,
    agent_id          UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sessions_count    INT NOT NULL DEFAULT 0,
    messages_count    INT NOT NULL DEFAULT 0,
    tokens_input      BIGINT NOT NULL DEFAULT 0,
    tokens_output     BIGINT NOT NULL DEFAULT 0,
    tokens_direct     BIGINT NOT NULL DEFAULT 0,
    tokens_subagent   BIGINT NOT NULL DEFAULT 0,
    model_calls       BIGINT NOT NULL DEFAULT 0,
    total_duration_ms BIGINT NOT NULL DEFAULT 0,
    est_cost_usd      NUMERIC(12,6) NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_start, agent_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_stats_hourly_agent ON agent_stats_hourly (agent_id, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_agent_stats_hourly_user ON agent_stats_hourly (user_id, bucket_start DESC);
