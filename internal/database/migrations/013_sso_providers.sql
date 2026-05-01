-- SSO provider configuration: platform-wide providers and per-org overrides

CREATE TABLE sso_providers (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope              text        NOT NULL CHECK (scope IN ('platform', 'org')),
    org_id             uuid        REFERENCES orgs(id) ON DELETE CASCADE,
    name               text        NOT NULL,
    provider_type      text        NOT NULL DEFAULT 'oidc',
    client_id          text        NOT NULL,
    client_secret_enc  text        NOT NULL,
    discovery_url      text        NOT NULL,
    allowed_domains    text[]      NOT NULL DEFAULT '{}',
    enabled            bool        NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sso_providers_org_scope CHECK (
        (scope = 'platform' AND org_id IS NULL) OR
        (scope = 'org'      AND org_id IS NOT NULL)
    )
);

CREATE TABLE org_platform_providers (
    org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    provider_id uuid NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    PRIMARY KEY (org_id, provider_id)
);

ALTER TABLE orgs ADD COLUMN sso_password_login bool NOT NULL DEFAULT true;

-- indexes for SSO provider lookups
CREATE INDEX idx_sso_providers_org ON sso_providers (org_id) WHERE org_id IS NOT NULL;
CREATE INDEX idx_org_platform_providers_provider ON org_platform_providers (provider_id);
CREATE INDEX idx_sso_providers_domains ON sso_providers USING GIN (allowed_domains);
