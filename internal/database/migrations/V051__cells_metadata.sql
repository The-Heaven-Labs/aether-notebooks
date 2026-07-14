-- Add metadata column to cells for storing chart config, title, description, etc.
ALTER TABLE cells ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';