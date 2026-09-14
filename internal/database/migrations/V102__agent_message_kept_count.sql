-- Migration 100: Compaction boundary kept-count
-- Records how many tail messages a compaction kept out of the summary so the
-- durable rebuild can retain them alongside the injected summary.
ALTER TABLE agent_messages ADD COLUMN IF NOT EXISTS kept_count INT;
