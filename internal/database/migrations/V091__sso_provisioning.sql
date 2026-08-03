ALTER TABLE sso_providers
  ADD COLUMN provisioning_mode text NOT NULL DEFAULT 'create_org'
    CHECK (provisioning_mode IN ('create_org', 'join_provider_org', 'deny')),
  ADD COLUMN default_role text NOT NULL DEFAULT 'non-admin'
    CHECK (default_role IN ('admin', 'non-admin', 'viewer'));
