CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE public_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('notebook', 'dashboard')),
    resource_id   UUID NOT NULL,
    token         TEXT NOT NULL UNIQUE DEFAULT encode(gen_random_bytes(16), 'hex'),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_public_tokens_org ON public_tokens (org_id);
CREATE INDEX idx_public_tokens_resource ON public_tokens (resource_type, resource_id);

-- Migrate existing dashboard tokens (migration ID 0 for legacy)
INSERT INTO public_tokens (org_id, resource_type, resource_id, token, created_at, created_by)
SELECT d.org_id, 'dashboard', d.id, d.public_token, NOW(), '00000000-0000-0000-0000-000000000000'
FROM dashboards d WHERE d.public_token IS NOT NULL;

ALTER TABLE dashboards DROP COLUMN public_token;

ALTER TABLE orgs ADD COLUMN public_sharing_enabled BOOLEAN NOT NULL DEFAULT true;
