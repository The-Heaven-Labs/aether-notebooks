/**
 * Built-in chart palettes.
 *
 * Kept ECharts-free (like `appChartTheme.ts`) so consumers that only need the
 * hex values — e.g. the app settings panel's preset picker — never pull the
 * chart renderer into their eager bundle.
 */

/** The global light-scheme palette (also the "Aether" app preset). */
export const LIGHT_CHART_COLORS = [
  '#6366f1', '#06b6d4', '#10b981', '#f59e0b',
  '#f43f5e', '#8b5cf6', '#0ea5e9', '#84cc16',
]

/** The global dark-scheme palette. */
export const DARK_CHART_COLORS = [
  '#818cf8', '#22d3ee', '#34d399', '#fbbf24',
  '#fb7185', '#a78bfa', '#38bdf8', '#a3e635',
]

/** A named chart-palette preset for the app settings picker. */
export interface ChartPalettePreset {
  value: string
  label: string
  /** The preset's colors; absent for Default (no override) and Custom. */
  colors?: string[]
}

/** The curated app-surface chart palettes offered by the settings picker. */
export const CHART_PALETTE_PRESETS: ChartPalettePreset[] = [
  { value: 'default', label: 'Default' },
  { value: 'aether', label: 'Aether', colors: LIGHT_CHART_COLORS },
  {
    value: 'amber', label: 'Amber',
    colors: ['#f59e0b', '#fb923c', '#f43f5e', '#8b5cf6', '#0ea5e9', '#84cc16', '#e879f9', '#14b8a6'],
  },
  {
    value: 'teal', label: 'Teal',
    colors: ['#14b8a6', '#06b6d4', '#0ea5e9', '#6366f1', '#8b5cf6', '#10b981', '#f59e0b', '#f43f5e'],
  },
  { value: 'custom', label: 'Custom' },
]
