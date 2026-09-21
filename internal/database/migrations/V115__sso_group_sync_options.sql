ALTER TABLE sso_providers
  ADD COLUMN sync_empty_groups  bool NOT NULL DEFAULT false,
  ADD COLUMN strip_group_prefix bool NOT NULL DEFAULT false;
