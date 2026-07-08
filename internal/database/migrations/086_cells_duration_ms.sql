-- Migration 086: Add duration_ms to cells for execution time persistence
-- Stores the last execution duration so timing survives page refresh.

ALTER TABLE cells ADD COLUMN duration_ms INT;
