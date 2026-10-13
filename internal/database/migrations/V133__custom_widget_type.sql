-- V133: custom widget type (data apps wave 4 — escape hatch tier 2).
-- Author HTML/CSS/JS; rendering is sandboxed client-side behind the org switch.
ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_type_check;
ALTER TABLE widgets ADD CONSTRAINT widgets_type_check
    CHECK (type IN ('chart', 'table', 'text', 'metric', 'html', 'custom'));
