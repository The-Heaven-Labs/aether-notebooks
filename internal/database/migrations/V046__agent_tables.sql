-- Migration 046: Agent System Tables

-- model_configs — Admin-created LLM endpoints
CREATE TABLE model_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    provider        TEXT NOT NULL CHECK (provider IN ('openai', 'openai-compatible')),
    base_url        TEXT NOT NULL,
    model           TEXT NOT NULL,
    api_key_encrypted BYTEA NOT NULL,
    default_params  JSONB NOT NULL DEFAULT '{}',
    context_window  INT NOT NULL DEFAULT 128000,
    folder_id       UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_model_configs_org ON model_configs (org_id);

-- skills — Reusable capability bundles
CREATE TABLE skills (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    description   TEXT,
    system_prompt  TEXT,
    tool_ids      TEXT[] NOT NULL DEFAULT '{}',
    folder_id     UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by    UUID NOT NULL REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_skills_org ON skills (org_id);

-- agents — The agent definition
CREATE TABLE agents (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                 UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name                   TEXT NOT NULL,
    description           TEXT,
    model_config_id        UUID REFERENCES model_configs(id) ON DELETE SET NULL,
    subagent_model_config_id UUID REFERENCES model_configs(id) ON DELETE SET NULL,
    system_prompt          TEXT,
    skill_ids              UUID[] NOT NULL DEFAULT '{}',
    mcp_servers            JSONB NOT NULL DEFAULT '[]',
    mcp_env_encrypted      BYTEA,
    folder_id              UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by              UUID NOT NULL REFERENCES users(id),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agents_org ON agents (org_id);

-- agent_sessions — One per notebook chat
CREATE TABLE agent_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    notebook_id UUID NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id),
    max_turns   INT NOT NULL DEFAULT 100,
    max_tokens  INT NOT NULL DEFAULT 100000,
    ended_at    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_sessions_lookup ON agent_sessions (agent_id, created_at DESC);

-- agent_messages — Chat history
CREATE TABLE agent_messages (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    role          TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content       TEXT,
    tool_call_id  UUID,
    tool_calls    JSONB,
    tokens_input  INT,
    tokens_output INT,
    model_calls   INT DEFAULT 1,
    duration_ms   INT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_messages_session ON agent_messages (session_id, created_at);

-- subagent_tasks — Parallel exploration
CREATE TABLE subagent_tasks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_session_id UUID NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    parent_message_id UUID,
    agent_id          UUID REFERENCES agents(id),
    goal              TEXT NOT NULL,
    context           JSONB,
    status            TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    result            JSONB,
    tokens_input      INT,
    tokens_output     INT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at      TIMESTAMPTZ
);

CREATE INDEX idx_subagent_tasks_session ON subagent_tasks (parent_session_id);

-- subagent_messages — Isolated message chain per subagent
CREATE TABLE subagent_messages (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subagent_task_id UUID NOT NULL REFERENCES subagent_tasks(id) ON DELETE CASCADE,
    role             TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content          TEXT,
    tool_call_id     UUID,
    tool_calls       JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subagent_messages_task ON subagent_messages (subagent_task_id, created_at);

-- agent_stats_daily — Metrics rollup
CREATE TABLE agent_stats_daily (
    date            DATE NOT NULL,
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    sessions_count INT NOT NULL DEFAULT 0,
    messages_count INT NOT NULL DEFAULT 0,
    tokens_input    BIGINT NOT NULL DEFAULT 0,
    tokens_output   BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (date, agent_id, user_id)
);

-- agent_versions — Self-improvement history
CREATE TABLE agent_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version         INT NOT NULL,
    name            TEXT,
    description     TEXT,
    system_prompt   TEXT,
    skill_ids       UUID[],
    model_config_id UUID,
    changed_by      UUID NOT NULL REFERENCES users(id),
    change_reason   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (agent_id, version)
);

CREATE INDEX idx_agent_versions_agent ON agent_versions (agent_id, version DESC);
