-- Add created_by to cell_versions for tracking who made changes
ALTER TABLE cell_versions ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id);