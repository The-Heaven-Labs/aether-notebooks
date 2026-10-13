/**
 * The single field-help source for the app-builder surfaces: every config
 * field the widget drawer and the App settings panel render, with the exact
 * strings the UI shows (inline hints and HelpTip popovers). The Go agent guide
 * (`internal/agent/guide/guide.md`) documents the same fields; a test asserts
 * every key here appears there, so the two can never drift apart.
 *
 * Voice: quiet instrument, second person — one line, an example where a value
 * is subtle (per the shape decisions).
 */
export interface FieldHelp {
  /** Short field name as the drawer labels it. */
  label: string
  /** One line: what the field does. */
  hint: string
  /** Optional concrete value to copy. */
  example?: string
}

/** Help keyed by widget type (or `app`) + dot-separated field path. */
export const fieldHelp: Record<string, FieldHelp> = {
  // ── metric ──────────────────────────────────────────────────────────────
  'metric.value_column': {
    label: 'Value column',
    hint: 'The numeric column the metric computes over.',
    example: 'revenue',
  },
  'metric.aggregation': {
    label: 'Aggregation',
    hint: 'How rows collapse to one number: sum, avg, min, max, median, count, count_distinct, first, or none (first row).',
    example: 'sum',
  },
  'metric.label': {
    label: 'Label',
    hint: 'Shown above the number. Without one, viewers see the column name.',
    example: 'Revenue',
  },
  'metric.format.style': {
    label: 'Format',
    hint: 'number keeps digits, currency prefixes a code, percent appends % to the value as-is (write 12.5, not 0.125).',
    example: 'currency',
  },
  'metric.format.currency': {
    label: 'Currency',
    hint: 'ISO code used when the style is currency.',
    example: 'USD',
  },
  'metric.format.decimals': {
    label: 'Decimals',
    hint: "Fraction digits, 0–6. Empty keeps Intl's default (2, or 1 for compact).",
    example: '0',
  },
  'metric.format.compact': {
    label: 'Compact',
    hint: 'Shortens large numbers (1.2M).',
    example: 'true',
  },
  'metric.comparison.column': {
    label: 'Comparison column',
    hint: 'A column holding the previous value; the change renders beside the metric.',
    example: 'prev_revenue',
  },
  'metric.comparison.mode': {
    label: 'Comparison mode',
    hint: 'delta shows the difference; percent shows the change ratio.',
    example: 'percent',
  },
  'metric.sparkline.x_column': {
    label: 'Sparkline X column',
    hint: 'Ordering column for the trend line (dates or numbers).',
    example: 'day',
  },
  'metric.sparkline.y_column': {
    label: 'Sparkline Y column',
    hint: 'Numeric column plotted in the trend line.',
    example: 'revenue',
  },

  // ── text ────────────────────────────────────────────────────────────────
  'text.markdown': {
    label: 'Markdown',
    hint: 'Markdown with live {{filters}} — values interpolate as plain text. Start with a heading (## …).',
    example: '## Events\n\nWindow: {{window}}',
  },

  // ── html ────────────────────────────────────────────────────────────────
  'html.html': {
    label: 'Panel HTML',
    hint: 'Script-free markup; scripts, links, forms, and third-party images are stripped.',
    example: '<div class="stat">42</div>',
  },
  'html.css': {
    label: 'Panel CSS',
    hint: 'Stylesheet for the panel; the app theme’s CSS variables are available.',
    example: '.stat { font-size: 28px; color: var(--accent); }',
  },

  // ── custom ──────────────────────────────────────────────────────────────
  'custom.html': {
    label: 'Component HTML',
    hint: 'Markup for the sandboxed component; scripts live in Component JS.',
    example: '<div id="root"></div>',
  },
  'custom.css': {
    label: 'Component CSS',
    hint: 'Stylesheet for the component; the app theme’s CSS variables are available.',
    example: '#root { color: var(--accent); }',
  },
  'custom.js': {
    label: 'Component JS',
    hint: 'Inline script (no network); use aether.requestData(key) for bound data.',
    example: "aether.requestData('sales')",
  },
  'custom.bindings': {
    label: 'Data bindings',
    hint: 'Widgets of this dashboard the component may read through the bridge, keyed by name.',
    example: 'sales → Revenue by day',
  },

  // ── table ───────────────────────────────────────────────────────────────
  'table.columns': {
    label: 'Table columns',
    hint: 'Per-column formats; unlisted columns render as stored.',
    example: 'revenue',
  },
  'table.columns.format': {
    label: 'Column format',
    hint: 'number, currency, or percent, with optional currency code, decimals, and compact.',
    example: 'currency',
  },

  // ── appearance ──────────────────────────────────────────────────────────
  'appearance.background': {
    label: 'Background',
    hint: 'card frames the widget on a surface; transparent lets the board show through.',
    example: 'card',
  },
  'appearance.border': {
    label: 'Border',
    hint: 'Draws the hairline around a card.',
    example: 'true',
  },
  'appearance.padding': {
    label: 'Padding',
    hint: 'Inner spacing: none, sm, or md.',
    example: 'sm',
  },
  'appearance.align': {
    label: 'Align',
    hint: 'Alignment for text-like content: left, center, or right.',
    example: 'left',
  },

  // ── chart ───────────────────────────────────────────────────────────────
  'chart.chart_type': {
    label: 'Chart type',
    hint: 'The chart to render; each type needs its own columns (see the guide).',
    example: 'bar',
  },
  'chart.x_column': {
    label: 'X column',
    hint: 'Category axis column.',
    example: 'day',
  },
  'chart.y_columns': {
    label: 'Y columns',
    hint: 'One or more value columns; each becomes a series.',
    example: 'revenue',
  },
  'chart.group_by': {
    label: 'Group by',
    hint: 'Splits rows into one series per group value.',
    example: 'region',
  },
  'chart.title': {
    label: 'Chart title',
    hint: 'Shown above the plot.',
    example: 'Revenue by day',
  },
  'chart.show_legend': {
    label: 'Show legend',
    hint: 'Lists the series beside the plot.',
    example: 'true',
  },
  'chart.series_colors': {
    label: 'Series colors',
    hint: 'Hex color per series name (y_columns, or group values with group by).',
    example: '{"revenue":"#7c5cff"}',
  },
  'chart.mark_lines': {
    label: 'Reference lines',
    hint: 'Dashed lines at a value, e.g. a target.',
    example: '[{"value":"1000","label":"Target","position":"horizontal"}]',
  },
  'chart.bar_mode': {
    label: 'Bar mode',
    hint: 'grouped, stacked, or horizontal.',
    example: 'stacked',
  },

  // ── app settings ────────────────────────────────────────────────────────
  'dashboard.grid_cols': {
    label: 'Grid columns',
    hint: 'Columns per chapter grid (default 12); widget layouts rescale proportionally when this changes.',
    example: '12',
  },
  'app.title': {
    label: 'App title',
    hint: 'Names the app in the masthead. Defaults to the dashboard title.',
    example: 'Q3 Revenue',
  },
  'app.subtitle': {
    label: 'Subtitle',
    hint: 'One line under the title — the question the app answers.',
    example: 'Sales ops · updated hourly',
  },
  'app.width': {
    label: 'Width',
    hint: 'fixed centers content at a reading width; full uses the viewport.',
    example: 'fixed',
  },
  'app.show_header': {
    label: 'Show header',
    hint: 'Hides the masthead for embedded surfaces.',
    example: 'true',
  },
  'app.show_refresh': {
    label: 'Show refresh',
    hint: 'Shows the refresh action in the masthead.',
    example: 'true',
  },
  'app.show_last_updated': {
    label: 'Show last updated',
    hint: 'Shows the freshness line under the title.',
    example: 'true',
  },
  'app.filter_scope': {
    label: 'Filter scope',
    hint: "all shows every filter on every chapter; tab shows each chapter only the filters its widgets use (plus the filters those depend on).",
    example: 'tab',
  },
  'app.theme.accent': {
    label: 'Accent',
    hint: 'Interactive accent; must clear 3:1 contrast against the canvas.',
    example: '#7c5cff',
  },
  'app.theme.background': {
    label: 'Background',
    hint: 'Page canvas; text must clear 4.5:1 contrast against it.',
    example: '#0b0b12',
  },
  'app.theme.surface': {
    label: 'Surface',
    hint: 'Card background; text must clear 4.5:1 contrast against it.',
    example: '#15151f',
  },
  'app.theme.text': {
    label: 'Text',
    hint: 'Primary text; must clear 4.5:1 contrast against both canvas and cards.',
    example: '#f2f2f7',
  },
  'app.theme.border': {
    label: 'Border',
    hint: 'Hairline color for cards and controls.',
    example: '#26263a',
  },
  'app.theme.radius': {
    label: 'Corner radius',
    hint: 'Corner radius, 0–24.',
    example: '8',
  },
  'app.theme.font_family': {
    label: 'Font family',
    hint: 'dm-sans, jetbrains-mono, or system.',
    example: 'dm-sans',
  },
  'app.theme.color_scheme': {
    label: 'Color scheme',
    hint: "auto follows the reader's theme; light/dark pin it.",
    example: 'dark',
  },
  'app.theme.chart_palette': {
    label: 'Chart palette',
    hint: 'Up to 12 hex colors used in order by charts.',
    example: '["#7c5cff","#22c55e"]',
  },
}

/** Convenience lookup for a field; undefined when the field has no help. */
export function fieldHelpFor(key: string): FieldHelp | undefined {
  return fieldHelp[key]
}
