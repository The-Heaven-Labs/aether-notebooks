-- Migration 059: Add agent_updated_at to cells table
-- Tracks when the agent last updated a cell's source, used to suppress
-- auto-save on the frontend after agent updates.

ALTER TABLE cells ADD COLUMN agent_updated_at TIMESTAMPTZ;

-- Partial index for quick lookups during auto-save suppression
CREATE INDEX idx_cells_agent_updated_at ON cells(agent_updated_at)
    WHERE agent_updated_at IS NOT NULL;
