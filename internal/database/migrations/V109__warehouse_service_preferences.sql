-- User-level routing preference: which explicitly granted service in a
-- warehouse this user's queries run on. Deleted with the connector so a
-- preference can never point at a removed endpoint.
CREATE TABLE warehouse_service_preferences (
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    connector_id UUID NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, warehouse_id)
);
