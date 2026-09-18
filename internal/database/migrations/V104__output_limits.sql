-- Bounded cell outputs: per-org byte caps enforced at execution time and on
-- the notebook read path. 0 means unlimited (explicit per-org opt-out); the
-- effective value is clamped by the platform ceiling AETHER_OUTPUT_LIMITS_MAX_BYTES.
ALTER TABLE orgs ADD COLUMN cell_output_max_bytes BIGINT NOT NULL DEFAULT 10485760;             -- 10MB per-cell cap
ALTER TABLE orgs ADD COLUMN notebook_inline_outputs_max_bytes BIGINT NOT NULL DEFAULT 33554432;  -- 32MB inline budget on notebook GET