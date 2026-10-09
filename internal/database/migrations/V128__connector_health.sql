-- Connector health: one outcome timeline per connector so the Connectors page
-- can render persisted status without probing data planes on load (D6-D9).
-- Additive and nullable; existing rows start as "Never used".
ALTER TABLE connectors ADD COLUMN IF NOT EXISTS last_success_at TIMESTAMPTZ;
ALTER TABLE connectors ADD COLUMN IF NOT EXISTS last_failure_at TIMESTAMPTZ;
ALTER TABLE connectors ADD COLUMN IF NOT EXISTS last_error TEXT;
