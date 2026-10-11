import type { DashboardVariable, DashboardVariableType } from '../types'

// Neutralize markdown link/image syntax so values stay inert: escape
// backslashes first, then the characters that could form links/images.
// Escaped punctuation renders as the literal character, so displayed text is
// unchanged. Residual, accepted behavior: emphasis (e.g. *text*) and
// autolinks (bare URLs) in values remain markdown-active; no XSS is possible
// because the Task 6 renderer never enables rehypeRaw, and react-markdown's
// default urlTransform strips javascript:/data: URLs.
function escapeMarkdown(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/\[/g, '\\[').replace(/\]/g, '\\]').replace(/!/g, '\\!')
}

function escapeHtml(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function displayValue(value: unknown, type: DashboardVariableType): string {
  if (value === null || value === undefined) return ''
  if (Array.isArray(value)) {
    // date_range round-trips as [start, end]; everything else joins.
    if (type === 'date_range' && value.length === 2 && typeof value[0] === 'string' && typeof value[1] === 'string') {
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
  const declared = new Map(variables.map((v): [string, DashboardVariable] => [v.name, v]))
  return template.replace(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g, (match, name: string) => {
    const variable = declared.get(name)
    if (!variable) return match
    return escapeHtml(escapeMarkdown(displayValue(values[name], variable.type)))
  })
}
