-- V132: html widget type (data apps wave 3 — escape hatch tier 1).
-- Script-free HTML/CSS panels; rendering is sandboxed client-side.
ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_type_check;
ALTER TABLE widgets ADD CONSTRAINT widgets_type_check
    CHECK (type IN ('chart', 'table', 'text', 'metric', 'html'));
