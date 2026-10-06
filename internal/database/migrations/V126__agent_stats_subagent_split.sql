-- Split subagent usage into input/output so the stats chart can render it as
-- its own series and the cost rollup can price it with the matching input and
-- output rates. tokens_subagent stays as the combined total for older
-- consumers; the rollup keeps all three in sync.
ALTER TABLE agent_stats_hourly
    ADD COLUMN IF NOT EXISTS tokens_subagent_input  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tokens_subagent_output BIGINT NOT NULL DEFAULT 0;

-- One-time backfill for buckets written before the split existed: recover the
-- input/output split from subagent_tasks and add the subagent portion to
-- est_cost_usd (direct-only buckets are untouched). Buckets whose source
-- subagent_tasks rows are gone keep a zero split. Ordering note: the binary
-- applies migrations before serving traffic or running the hourly rollup,
-- which recomputes the last ~25h with the full formula anyway.
UPDATE agent_stats_hourly h
SET tokens_subagent = s.sub_in + s.sub_out,
    tokens_subagent_input = s.sub_in,
    tokens_subagent_output = s.sub_out,
    est_cost_usd = h.est_cost_usd
        + (s.sub_in * COALESCE(mc.price_per_input_token, 0)
           + s.sub_out * COALESCE(mc.price_per_output_token, 0)) / 1000000.0
FROM (
    SELECT date_trunc('hour', COALESCE(st.completed_at, st.created_at)) AS bucket,
           se.agent_id,
           se.user_id,
           COALESCE(SUM(st.tokens_input), 0) AS sub_in,
           COALESCE(SUM(st.tokens_output), 0) AS sub_out
    FROM subagent_tasks st
    JOIN agent_sessions se ON se.id = st.parent_session_id
    GROUP BY 1, 2, 3
) s
LEFT JOIN agents a ON a.id = s.agent_id
LEFT JOIN model_configs mc ON mc.id = a.model_config_id
WHERE h.bucket_start = s.bucket
  AND h.agent_id = s.agent_id
  AND h.user_id = s.user_id;
