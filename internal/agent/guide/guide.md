# App Builder Guide

How to build an Aether data app: chapters, filters, widgets, theming, and
publishing. Read this before composing a dashboard with the agent tools; the
review_dashboard tool checks the same rules this guide teaches.

## Composition

An app reads top to bottom as one argument:

- **Lede** — the app title and subtitle state the question the app answers.
- **Readings** — metrics and charts that answer it.
- **Evidence** — tables and text panels that let a reader verify the answer.

Chapters split the argument into acts. Prefer 3–5 chapters, each with one
reading and its evidence. A chapter with no widgets breaks the story; a metric
without a label makes the reader guess. Text panels open a chapter with a
markdown heading (`## …`), not a bare paragraph.

The chapter grids sit on `settings.grid_cols` (default 12, editor: App settings
→ Grid); changing it rescales widget layouts proportionally.

## Widget types

Widgets are `chart`, `table`, `metric`, `text`, or `html`. A widget reads a
notebook cell (`notebook_id` + `cell_id`) or a query (`connector_id` + `query`);
text and html panels need neither.

### metric

One number computed client-side over the query's rows, with an optional
comparison and sparkline. Set it with `set_widget_config`:

```json
{
  "metric": {
    "value_column": "revenue",
    "aggregation": "sum",
    "label": "Revenue",
    "format": { "style": "currency", "currency": "USD", "decimals": 0, "compact": true },
    "comparison": { "column": "prev_revenue", "mode": "percent" },
    "sparkline": { "x_column": "day", "y_column": "revenue" }
  }
}
```

- `aggregation`: `none` (first row), `sum`, `avg`, `min`, `max`, `median`,
  `count`, `count_distinct`, `first`.
- `format.style`: `number`, `currency`, `percent` (values render as-is with a
  `%` suffix — write `12.5`, not `0.125`).
- `comparison.mode`: `delta` or `percent`; the column is the previous value.
- Always set `label`: the review flags metrics without one.

### text

Markdown with live filters: `{{variable}}` tokens interpolate the current
values (escaped as plain text, never raw HTML). A date_range renders as
`start → end`. Unknown tokens stay visible so typos are easy to spot.

```json
{ "text": { "markdown": "## Events\n\nWindow: {{window}}" } }
```

### html

A script-free panel for bespoke sections: your markup + stylesheet, rendered
in a sandboxed frame. Source-less like a text panel. Set it with
`set_widget_config`'s `html` block (or create with `type: "html"`):

```json
{ "html": "<div class=\"stat\">42</div>", "css": ".stat { font-size: 28px; color: var(--accent); }" }
```

- Scripts, forms, links, and third-party images are stripped; images may use
  `data:` URIs or same-origin attachments only.
- `{{variable}}` tokens interpolate the current values as escaped text.
- The app theme's CSS variables (`var(--accent)`, `var(--text-primary)`, …)
  are available inside the panel.
- Org admins can disable panels org-wide (`html_panels_enabled`); creating one
  while disabled fails — fall back to `text`.

### table

A query's rows, with optional per-column formats:

```json
{
  "table": {
    "columns": [
      { "name": "revenue", "format": { "style": "currency", "currency": "USD", "decimals": 2 } },
      { "name": "margin", "format": { "style": "percent", "decimals": 1 } },
      { "name": "notes" }
    ]
  }
}
```

Columns without a format render as stored. The review notes tables with no
column formats.

### appearance

Every widget accepts an `appearance` block (set with `set_dashboard_layout`):
`background` (`card` frames the widget, `transparent` lets the board show
through), `border` (draws the hairline on cards), `padding` (`none`, `sm`,
`md`), and `align` (`left`, `center`, `right` for text-like content).

### chart

All 14 chart types use `create_chart`'s vocabulary: `bar`, `line`, `area`,
`scatter`, `pie`, `donut`, `timeline`, `hierarchy_tree`, `big_number`, `map`,
`sankey`, `funnel`, `heatmap`, `histogram`. Set the stored config with
`set_widget_config`'s `chart` block (or `create_dashboard_widget`'s):

```json
{
  "chart": {
    "chart_type": "bar",
    "x_column": "day",
    "y_columns": ["revenue", "cost"],
    "group_by": "region",
    "bar_mode": "stacked",
    "title": "Revenue by day",
    "show_legend": true,
    "series_colors": { "revenue": "#7c5cff" },
    "mark_lines": [{ "value": "1000", "label": "Target", "position": "horizontal" }]
  }
}
```

Type-specific requirements and options:

| Type | Requires | Notable options |
|---|---|---|
| `bar` | `x_column`, `y_columns` | `bar_mode`: `grouped`, `stacked`, `horizontal` |
| `line` / `area` | `x_column`, `y_columns` | `area_mode`: `area`, `stacked`; `smooth`, `connect_nulls` |
| `scatter` | `x_column`, `y_columns` | `color_column`, `size_column` |
| `pie` / `donut` | `x_column` or `label_column`, `y_columns` | `rose_type`, `start_angle`, `pad_angle` |
| `timeline` | `time_column`, `label_column` | `end_time_column`, `group_by`, `show_time_deltas` |
| `hierarchy_tree` | `id_column`, `parent_id_column` | `metric_columns`, `layout` |
| `big_number` | `value_column` | `label`, `prefix`, `suffix`, `decimal_places` |
| `map` | `lat_column`, `lon_column` | `label_column`, `show_labels` |
| `sankey` | `x_column` (source), `y_columns` `[target, value]` | `node_align`, `node_width`, `node_gap` |
| `funnel` | `category_column`, `value_column` | `funnel_sort`, `suffix` |
| `heatmap` | `x_column`, `y_axis_column`, `value_column` | `show_labels` |
| `histogram` | `value_column` | `bin_count` |

Shared options: `title`, `show_labels`, `show_legend`, `show_grid`,
`skip_empty`, `series_colors` (hex per series), and — on bar/line/area/scatter
— `mark_lines` (dashed reference lines) and `log_scale`/`data_zoom` where
available.

## Chapters

Chapters live in `settings.tabs` (≤10: `{id, title}`, ids like
`overview`, `q3-orders`). A widget joins a chapter via
`widgets.layout.tab`; an empty or unknown tab falls back to the first
chapter. Each chapter is its own grid: identical coordinates in different
chapters coexist, and the app compacts a chapter's leading rows.

- Create chapters with `update_dashboard_settings` (`tabs`) or
  `set_dashboard_layout`.
- Place widgets with `set_dashboard_layout` (`placements`).
- Keep chapters balanced: the review flags a chapter with more than 3× the
  median widget count, and chapters with no widgets.

## Filter scoping

Variables (`settings.variables`) render in the filter strip and interpolate
server-side as `{{name}}` (date ranges as `{{name_start}}`/`{{name_end}}`).
Query-backed options (`options.mode: "query"`) load choices from SQL and may
depend on other variables via `depends_on`.

`settings.app.filter_scope` controls the strip:

- `"all"` (default): every filter shows on every chapter.
- `"tab"`: each chapter shows only the filters its widgets use — direct SQL
  and text-panel references, plus the transitive closure over options queries
  and `depends_on`, so a visible filter never hides the filters that feed it.
  Filters set elsewhere keep their values and stay applied in their chapters;
  the strip hints "n filters set in other chapters".

Under chapter scoping, a variable no widget uses can never appear; the review
flags it.

## Theming

`settings.app.theme` overrides the app surface's tokens (org branding is the
fallback layer):

```json
{
  "app": {
    "title": "Q3 Revenue",
    "subtitle": "Sales ops · updated hourly",
    "width": "full",
    "show_header": true,
    "show_refresh": true,
    "show_last_updated": true,
    "theme": {
      "accent": "#7c5cff",
      "background": "#0b0b12",
      "surface": "#15151f",
      "text": "#f2f2f7",
      "border": "#26263a",
      "radius": 8,
      "font_family": "dm-sans",
      "color_scheme": "dark",
      "chart_palette": ["#7c5cff", "#22c55e", "#f59e0b"]
    }
  }
}
```

Guardrails (the review checks these): text must clear 4.5:1 contrast against
both the canvas (`background`) and cards (`surface`); accent must clear 3:1
against the canvas. Failing tokens fall back per token, but fix the authored
pair instead of relying on the fallback. Fonts: `dm-sans`, `jetbrains-mono`,
`system`. Radius: 0–24. Chart palettes: ≤12 hex colors.

## Publishing

- The **draft** is the live co-edited document; the app surface shows it to
  editors only.
- `publish_dashboard` snapshots the draft as an immutable version. Viewers
  and public links see the latest published version until the next publish.
- The versions panel reports **unpublished changes** (draft content hash ≠
  latest version). Restoring a version copies it into the draft and never
  publishes.
- `settings.public_live` (public pages) allows embedded queries to run live,
  as the dashboard creator, rate-limited per token; off means snapshot-only.
- Publish refuses a draft whose widgets fail projection validation (the
  warnings surface in the versions panel and review).

## Troubleshooting

The review_dashboard tool (and the editor's App health panel) lists findings
with the tool call that fixes each:

| Finding | Fix |
|---|---|
| widgets overlap / exceed the grid | `set_dashboard_layout` (move or resize) |
| chapter has no widgets | `set_dashboard_layout` (place a widget) |
| chapter starts at a high row | `set_dashboard_layout` (the app compacts it, but authored coordinates should start at 0) |
| widget has no chapter / names a removed one | `set_dashboard_layout` (placements) |
| metric has no value column / label | `set_widget_config` |
| table has no column formats | `set_widget_config` |
| text panel is empty / has no heading | `set_widget_config` |
| app has no title | `update_dashboard_settings` |
| theme contrast fails | `update_dashboard_settings` |
| filter never shows under chapter scoping | `update_dashboard_settings` (scope or usage) |
| unpublished changes | `publish_dashboard` |
| projection warnings | fix the named widget, then publish |

Checklist before publishing: every chapter has a reading and evidence; every
metric is labeled; filters are scoped deliberately; the theme passes contrast;
review_dashboard reports no errors.
