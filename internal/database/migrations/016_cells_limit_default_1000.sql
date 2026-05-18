-- Add DEFAULT 1000 to the limit column so new cells get LIMIT 1000 automatically

ALTER TABLE cells ALTER COLUMN "limit" SET DEFAULT 1000;