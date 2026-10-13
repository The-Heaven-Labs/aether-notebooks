import { describe, test, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fieldHelp } from './fieldHelp'

// Every config field the widget drawer renders, plus the App settings panel's
// fields. Coverage is pinned here so a new field cannot ship without help.
const REQUIRED_FIELDS = [
  'metric.value_column', 'metric.aggregation', 'metric.label',
  'metric.format.style', 'metric.format.currency', 'metric.format.decimals', 'metric.format.compact',
  'metric.comparison.column', 'metric.comparison.mode',
  'metric.sparkline.x_column', 'metric.sparkline.y_column',
  'text.markdown',
  'table.columns', 'table.columns.format',
  'appearance.background', 'appearance.border', 'appearance.padding', 'appearance.align',
  'chart.chart_type', 'chart.x_column', 'chart.y_columns', 'chart.group_by',
  'chart.title', 'chart.show_legend', 'chart.series_colors', 'chart.mark_lines', 'chart.bar_mode',
  'dashboard.grid_cols',
  'app.title', 'app.subtitle', 'app.width',
  'app.show_header', 'app.show_refresh', 'app.show_last_updated', 'app.filter_scope',
  'app.theme.accent', 'app.theme.background', 'app.theme.surface', 'app.theme.text',
  'app.theme.border', 'app.theme.radius', 'app.theme.font_family', 'app.theme.color_scheme',
  'app.theme.chart_palette',
]

describe('fieldHelp', () => {
  test('covers every config field the drawer and app panel render', () => {
    for (const key of REQUIRED_FIELDS) {
      const help = fieldHelp[key]
      expect(help, `${key} must have help`).toBeDefined()
      expect(help.label.trim().length, `${key} needs a label`).toBeGreaterThan(0)
      expect(help.hint.trim().length, `${key} needs a hint`).toBeGreaterThan(0)
    }
  })

  test('every entry keeps the {label, hint, example?} shape', () => {
    for (const [key, help] of Object.entries(fieldHelp)) {
      expect(typeof help.label, key).toBe('string')
      expect(typeof help.hint, key).toBe('string')
      if (help.example !== undefined) expect(typeof help.example, key).toBe('string')
      // One line, quiet instrument: no trailing periods stacked into paragraphs.
      expect(help.hint.length, `${key} hint should stay one line`).toBeLessThan(220)
    }
  })

  test('every field is documented in the agent guide', () => {
    // Vitest runs with cwd = web/; the guide lives at the repo root's agent
    // package (the single source the Go tool embeds).
    const guide = readFileSync(resolve(process.cwd(), '../internal/agent/guide/guide.md'), 'utf8')
    for (const key of Object.keys(fieldHelp)) {
      const field = key.split('.').pop() as string
      expect(guide.includes(field), `${key} (${field}) must appear in the agent guide`).toBe(true)
    }
  })
})
