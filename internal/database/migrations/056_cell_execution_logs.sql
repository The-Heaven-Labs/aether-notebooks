-- +migrate Up
CREATE TABLE IF NOT EXISTS cell_execution_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  cell_id UUID NOT NULL,
  notebook_id UUID NOT NULL,
  connector_id UUID,
  connect_time_ms INT,
  query_time_ms INT,
  render_time_ms INT,
  queue_time_ms INT,
  total_time_ms INT,
  row_count INT,
  executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cell_execution_logs_cell_id ON cell_execution_logs(cell_id);
CREATE INDEX IF NOT EXISTS idx_cell_execution_logs_notebook_id ON cell_execution_logs(notebook_id);

-- +migrate Down
DROP TABLE IF EXISTS cell_execution_logs;
