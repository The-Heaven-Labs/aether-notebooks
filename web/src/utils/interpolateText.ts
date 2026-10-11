import type { DashboardVariable } from '../types'

function escapeHtml(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function displayValue(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (Array.isArray(value)) {
    // date_range round-trips as [start, end]; everything else joins.
    if (value.length === 2 && typeof value[0] === 'string' && /^\d{4}-\d{2}-\d{2}/.test(value[0])) {
      return `${value[0]} → ${value[1]}`
    }
    return value.map(v => String(v)).join(', ')
  }
  return String(value)
}

// Display-only interpolation for text widgets: declared variables are
// substituted as escaped plain text. Unknown tokens stay visible so authors
// can spot typos; this never becomes a rendering surface because the text
// renderer never enables raw HTML.
export function interpolateText(
  template: string,
  values: Record<string, unknown>,
  variables: DashboardVariable[],
): string {
  const declared = new Set(variables.map(v => v.name))
  return template.replace(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g, (match, name: string) => {
    if (!declared.has(name)) return match
    return escapeHtml(displayValue(values[name]))
  })
}
