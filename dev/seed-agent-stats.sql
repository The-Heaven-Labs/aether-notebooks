-- Seed demo agent usage stats for the Heaven Labs dev org.
-- Makes the Agent Usage page (StatsPage) render charts with enough series to
-- exercise legend layout. Idempotent: demo agents are matched by name, hourly
-- buckets are upserted by primary key, and values are deterministic.
--
-- Usage:
--   task dev:seed-stats

DO $$
DECLARE
  v_org     uuid;
  v_admin   uuid;
  v_config  uuid;
  v_members int;
BEGIN
  SELECT id INTO v_org FROM orgs WHERE slug = 'heaven-labs';
  IF v_org IS NULL THEN
    RAISE NOTICE 'org "heaven-labs" not found - skipping agent stats seed';
    RETURN;
  END IF;

  SELECT count(*) INTO v_members FROM org_members WHERE org_id = v_org;
  IF v_members = 0 THEN
    RAISE NOTICE 'no members in "heaven-labs" - skipping agent stats seed';
    RETURN;
  END IF;

  SELECT user_id INTO v_admin FROM org_members
   WHERE org_id = v_org AND role = 'admin' LIMIT 1;
  IF v_admin IS NULL THEN
    SELECT user_id INTO v_admin FROM org_members WHERE org_id = v_org LIMIT 1;
  END IF;

  SELECT id INTO v_config FROM model_configs WHERE org_id = v_org LIMIT 1;

  -- Demo agents, matched by name so re-runs don't duplicate them.
  INSERT INTO agents (org_id, name, description, model_config_id, created_by)
  SELECT v_org, d.name, d.description, v_config, v_admin
  FROM (VALUES
    ('Analytics Copilot', 'Answers ad-hoc questions over the warehouse'),
    ('SQL Assistant',     'Drafts and explains SQL for analysts'),
    ('Data Explorer',     'Profiles tables and suggests joins'),
    ('Metrics Agent',     'Tracks KPIs and writes weekly summaries'),
    ('Report Writer',     'Turns query results into narrative reports')
  ) AS d(name, description)
  WHERE NOT EXISTS (SELECT 1 FROM agents a WHERE a.org_id = v_org AND a.name = d.name);

  -- Hourly usage for the last 7 days for every agent in the org, so the hour,
  -- today, 7d, and 30d ranges all have data. Values are deterministic so
  -- repeated runs stay stable.
  INSERT INTO agent_stats_hourly (
    bucket_start, agent_id, user_id,
    sessions_count, messages_count,
    tokens_input, tokens_output, tokens_direct, tokens_subagent,
    model_calls, total_duration_ms, est_cost_usd
  )
  SELECT
    date_trunc('hour', now()) - make_interval(hours => g.h),
    a.id,
    m.user_id,
    CASE WHEN g.h % 6 = 0 THEN 1 ELSE 0 END,
    1 + ((g.h + a.agent_rank) % 4),
    600 + ((g.h * 131 + a.agent_rank * 977 + (g.h / 24) * 53) % 5200),
    400 + ((g.h * 173 + a.agent_rank * 733 + (g.h / 24) * 91) % 3800),
    (g.h * 59 + a.agent_rank * 311) % 900,
    (g.h * 29 + a.agent_rank * 199) % 400,
    1 + ((g.h + a.agent_rank) % 3),
    45000 + ((g.h * 913 + a.agent_rank * 3571 + (g.h / 24) * 113) % 240000),
    ((600 + ((g.h * 131 + a.agent_rank * 977 + (g.h / 24) * 53) % 5200)) * 3.0
     + (400 + ((g.h * 173 + a.agent_rank * 733 + (g.h / 24) * 91) % 3800)) * 15.0) / 1000000.0
  FROM generate_series(0, 167) AS g(h)
  CROSS JOIN LATERAL (
    SELECT id, row_number() OVER (ORDER BY created_at, id) - 1 AS agent_rank
    FROM agents WHERE org_id = v_org
  ) AS a
  CROSS JOIN LATERAL (
    SELECT user_id FROM org_members
    WHERE org_id = v_org
    ORDER BY user_id
    OFFSET ((g.h + a.agent_rank) % v_members) LIMIT 1
  ) AS m
  ON CONFLICT (bucket_start, agent_id, user_id) DO UPDATE SET
    sessions_count = EXCLUDED.sessions_count,
    messages_count = EXCLUDED.messages_count,
    tokens_input = EXCLUDED.tokens_input,
    tokens_output = EXCLUDED.tokens_output,
    tokens_direct = EXCLUDED.tokens_direct,
    tokens_subagent = EXCLUDED.tokens_subagent,
    model_calls = EXCLUDED.model_calls,
    total_duration_ms = EXCLUDED.total_duration_ms,
    est_cost_usd = EXCLUDED.est_cost_usd;
END $$;
