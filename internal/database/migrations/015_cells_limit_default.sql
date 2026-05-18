-- Set default limit of 1000 rows for existing code cells

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'cells' AND column_name = 'limit') THEN
        UPDATE cells SET "limit" = 1000 WHERE type = 'code' AND "limit" IS NULL;
    END IF;
END $$;