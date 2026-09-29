-- V121: dashboard query widgets + dashboard variables.

-- Query-backed widgets: a widget may reference a connector + SQL instead of
-- a notebook cell. Cell-linked widgets keep notebook_id/cell_id.
ALTER TABLE widgets
    ADD COLUMN IF NOT EXISTS connector_id UUID NULL REFERENCES connectors(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS query TEXT NULL,
    ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT 'sql';

ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_source_check;
ALTER TABLE widgets ADD CONSTRAINT widgets_source_check CHECK (
    connector_id IS NULL OR (notebook_id IS NULL AND cell_id IS NULL AND query IS NOT NULL)
);

-- Convert legacy input widgets into dashboard variables. Markers let the
-- migration test execute exactly this block.
-- +conversion:start
WITH converted AS (
    SELECT DISTINCT ON (w.dashboard_id, COALESCE(NULLIF(w.config->>'paramName', ''), w.id::text))
        w.dashboard_id,
        COALESCE(NULLIF(w.config->>'paramName', ''), w.id::text) AS var_name,
        COALESCE(NULLIF(w.config->>'label', ''), NULLIF(w.config->>'paramName', ''), w.id::text) AS var_label,
        CASE w.type
            WHEN 'date_picker' THEN 'date'
            WHEN 'date_range' THEN 'date_range'
            WHEN 'number' THEN 'number'
            WHEN 'multi_select' THEN 'multi_select'
            ELSE 'text'
        END AS var_type,
        CASE
            WHEN w.type = 'multi_select' AND jsonb_typeof(w.config->'options') = 'array' THEN
                jsonb_build_object(
                    'mode', 'static',
                    'values', (
                        SELECT COALESCE(
                            jsonb_agg(jsonb_build_object('label', v, 'value', v)) FILTER (WHERE v <> ''),
                            '[]'::jsonb)
                        FROM jsonb_array_elements_text(w.config->'options') AS v
                    )
                )
        END AS options_json
    FROM widgets w
    WHERE w.type IN ('date_picker', 'date_range', 'freetext', 'number', 'multi_select')
    ORDER BY w.dashboard_id, COALESCE(NULLIF(w.config->>'paramName', ''), w.id::text), w.created_at
),
aggregated AS (
    SELECT dashboard_id,
           jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
               'name', var_name,
               'label', var_label,
               'type', var_type,
               'options', options_json
           ))) AS variables
    FROM converted
    GROUP BY dashboard_id
)
UPDATE dashboards d
SET settings = jsonb_set(COALESCE(d.settings, '{}'::jsonb), '{variables}', a.variables, true)
FROM aggregated a
WHERE d.id = a.dashboard_id;

DELETE FROM widgets
WHERE type IN ('date_picker', 'date_range', 'freetext', 'number', 'multi_select');
-- +conversion:end

-- Input widget types are no longer valid; they are variables now.
ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_type_check;
ALTER TABLE widgets ADD CONSTRAINT widgets_type_check
    CHECK (type IN ('chart', 'table', 'text', 'metric'));

-- parameter_overrides was never implemented; variables replace it.
UPDATE dashboards SET settings = settings - 'parameter_overrides'
WHERE settings ? 'parameter_overrides';
