-- Migration 122: OAuth 2.1 authorization server tables for the MCP endpoint
-- (RFC 7591 dynamic client registration, single-use auth codes, rotating
-- refresh tokens with reuse detection).

CREATE TABLE oauth_clients (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id     TEXT NOT NULL UNIQUE,
    client_name   TEXT NOT NULL DEFAULT '',
    redirect_uris TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ
);

CREATE TABLE oauth_auth_codes (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash        TEXT NOT NULL UNIQUE,
    client_id        TEXT NOT NULL,
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id           UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    scopes           TEXT[] NOT NULL DEFAULT '{}',
    resource         TEXT NOT NULL DEFAULT '',
    redirect_uri     TEXT NOT NULL DEFAULT '',
    code_challenge   TEXT NOT NULL,
    challenge_method TEXT NOT NULL DEFAULT 'S256',
    expires_at       TIMESTAMPTZ NOT NULL,
    used_at          TIMESTAMPTZ
);

CREATE TABLE oauth_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id    TEXT NOT NULL,
    refresh_hash TEXT NOT NULL UNIQUE,
    client_id    TEXT NOT NULL,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id       UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    resource     TEXT NOT NULL DEFAULT '',
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    replaced_by  UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_oauth_tokens_family ON oauth_tokens (family_id);
CREATE INDEX idx_oauth_tokens_user ON oauth_tokens (user_id);
