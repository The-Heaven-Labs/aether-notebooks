-- Records the master-key fingerprint applied by the last successful warehouse
-- reconcile. ClickHouse stores salted password hashes that cannot be
-- compared, so a fingerprint mismatch (or NULL) drives password re-issuance:
-- the next reconcile emits ALTER USER ... IDENTIFIED for every provisioned
-- user and stores the new fingerprint only after all statements succeed.
ALTER TABLE warehouses ADD COLUMN applied_master_fp TEXT;
