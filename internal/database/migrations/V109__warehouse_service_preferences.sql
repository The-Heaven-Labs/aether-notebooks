-- User-level routing preference: which explicitly granted service in a
-- warehouse this user's queries run on.
--
-- Cleanup semantics:
--   * user_id and warehouse_id FKs CASCADE on hard delete; users and
--     warehouses are hard-deleted.
--   * connectors are soft-deleted (deleted_at is set), so the connector_id
--     FK never fires in normal operation. The connector soft-delete handler
--     must delete preference rows explicitly (later task), and resolution
--     plus listing must filter connectors.deleted_at IS NULL.
--   * warehouse membership of the preferred connector is handler-validated
--     (later task), not enforced by a composite FK: connectors.warehouse_id
--     is ON DELETE SET NULL, so a composite FK combined with that RI action
--     makes warehouse deletion ordering-dependent and can fail.
CREATE TABLE warehouse_service_preferences (
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    connector_id UUID NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, warehouse_id)
);
