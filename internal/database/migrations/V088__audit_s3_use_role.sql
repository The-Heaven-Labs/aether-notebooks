ALTER TABLE platform_audit_s3_config ADD COLUMN IF NOT EXISTS use_role BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS audit_s3_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL DEFAULT '',
    region TEXT NOT NULL DEFAULT 'us-east-1',
    bucket TEXT NOT NULL DEFAULT '',
    access_key TEXT NOT NULL DEFAULT '',
    secret_key TEXT NOT NULL DEFAULT '',
    use_role BOOLEAN NOT NULL DEFAULT false,
    batch_size INT NOT NULL DEFAULT 100,
    flush_interval_secs INT NOT NULL DEFAULT 60,
    enabled BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(org_id)
);
