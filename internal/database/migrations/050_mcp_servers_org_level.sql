-- Migration 050: Move MCP servers from per-agent JSONB to org-level table

-- Create org-level mcp_servers table
CREATE TABLE mcp_servers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK (type IN ('stdio', 'http')),
    command     TEXT NOT NULL,
    args        TEXT[] NOT NULL DEFAULT '{}',
    created_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_mcp_servers_org ON mcp_servers (org_id);
CREATE UNIQUE INDEX uniq_mcp_servers_org_name ON mcp_servers (org_id, name);

-- Create agent_mcp_servers join table
CREATE TABLE agent_mcp_servers (
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    mcp_server_id UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, mcp_server_id)
);

-- Migrate existing JSONB mcp_servers data into the new tables.
-- Strategy: deduplicate by (org_id, name). When multiple agents have the same
-- name but different (type, command, args), append __2, __3, etc. to the name.
-- Then link each agent to its corresponding mcp_server row.

-- Step 1: Insert deduplicated mcp_servers with suffix for conflicting names
INSERT INTO mcp_servers (id, org_id, name, type, command, args, created_by)
SELECT
    gen_random_uuid(),
    dedup.org_id,
    dedup.final_name,
    dedup.type,
    dedup.command,
    dedup.args,
    dedup.created_by
FROM (
    SELECT DISTINCT ON (org_id, final_name)
        org_id, final_name, type, command, args, created_by
    FROM (
        SELECT
            a.org_id,
            CASE
                WHEN row_number() OVER (
                    PARTITION BY a.org_id, ms->>'name'
                    ORDER BY ms->>'type', ms->>'command', ms->>'args'
                ) = 1 THEN ms->>'name'
                ELSE ms->>'name' || '__' || row_number() OVER (
                    PARTITION BY a.org_id, ms->>'name'
                    ORDER BY ms->>'type', ms->>'command', ms->>'args'
                )
            END AS final_name,
            ms->>'type' AS type,
            ms->>'command' AS command,
            COALESCE(ms->'args', '[]'::jsonb) AS args,
            a.created_by
        FROM agents a
        CROSS JOIN LATERAL jsonb_array_elements(a.mcp_servers) AS ms
        WHERE a.mcp_servers IS NOT NULL AND a.mcp_servers != '[]'::jsonb
    ) AS expanded
) AS dedup
ON CONFLICT DO NOTHING;

-- Step 2: Link each agent to its mcp_servers by matching on (org_id, final_name, type, command)
-- For each agent's original mcp_servers entry, we reconstruct the final_name the same way
INSERT INTO agent_mcp_servers (agent_id, mcp_server_id)
SELECT
    ranked.agent_id,
    s.id
FROM (
    SELECT
        a.id AS agent_id,
        CASE
            WHEN row_number() OVER (
                PARTITION BY a.org_id, ms->>'name'
                ORDER BY ms->>'type', ms->>'command', ms->>'args'
            ) = 1 THEN ms->>'name'
            ELSE ms->>'name' || '__' || row_number() OVER (
                PARTITION BY a.org_id, ms->>'name'
                ORDER BY ms->>'type', ms->>'command', ms->>'args'
            )
        END AS final_name,
        ms->>'type' AS type,
        ms->>'command' AS command
    FROM agents a
    CROSS JOIN LATERAL jsonb_array_elements(a.mcp_servers) AS ms
    WHERE a.mcp_servers IS NOT NULL AND a.mcp_servers != '[]'::jsonb
) AS ranked
JOIN mcp_servers s ON s.org_id = (SELECT org_id FROM agents WHERE id = ranked.agent_id)
                   AND s.name = ranked.final_name
                   AND s.type = ranked.type
                   AND s.command = ranked.command
ON CONFLICT DO NOTHING;

-- Drop old JSONB columns from agents
ALTER TABLE agents DROP COLUMN mcp_servers;
ALTER TABLE agents DROP COLUMN IF EXISTS mcp_env_encrypted;