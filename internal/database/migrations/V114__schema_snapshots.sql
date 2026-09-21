-- Raw ClickHouse catalog metadata observed per connector, keyed by connector so
-- multiple services in one warehouse contribute without colliding. The
-- new-tables inbox derives "tables first observed since the last grant review"
-- from first_seen_at; last_seen_at is refreshed on each observation so a future
-- prune can distinguish live catalog entries from vanished ones. Rows are never
-- filtered per user here: per-subject filtering happens against
-- warehouse_table_grants when the inbox is read.
CREATE TABLE schema_snapshots (
    connector_id  UUID NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
    database_name TEXT NOT NULL,
    table_name    TEXT NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (connector_id, database_name, table_name)
);

CREATE INDEX idx_schema_snapshots_table ON schema_snapshots (database_name, table_name);
