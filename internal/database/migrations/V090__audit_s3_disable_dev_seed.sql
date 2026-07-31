-- Disable the dev-seeded Garage audit S3 config by default.
-- The dev seed (SeedDevAuditS3Config) now inserts enabled=false; this one-time
-- migration resets rows seeded by earlier versions so the audit S3 writer does
-- not auto-start against the dev Garage endpoint. Rows pointing to any other
-- endpoint (production-configured) are left untouched.
UPDATE platform_audit_s3_config
SET enabled = false
WHERE endpoint = 'http://aether-garage:3900' AND enabled = true;
