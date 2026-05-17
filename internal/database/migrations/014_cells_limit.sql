-- Add limit column to cells table for query result row limiting

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'cells' AND column_name = 'limit') THEN
        ALTER TABLE cells ADD COLUMN "limit" INTEGER;
    END IF;
END $$;