ALTER TABLE sso_providers
  ADD COLUMN scopes           text[] NOT NULL DEFAULT '{}',
  ADD COLUMN groups_claim     text   NOT NULL DEFAULT 'groups',
  ADD COLUMN group_prefix     text   NOT NULL DEFAULT '',
  ADD COLUMN auto_sync_groups bool   NOT NULL DEFAULT false,
  ADD COLUMN get_user_info    bool   NOT NULL DEFAULT false;

CREATE TABLE sso_group_memberships (
    provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    group_id    UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_id, group_id, user_id)
);

CREATE INDEX idx_sso_group_memberships_user ON sso_group_memberships (user_id);
