/**
 * Reads an html panel's markup and stylesheet out of a widget config. Absent
 * or malformed values yield `''` so callers can render the empty hint.
 */
export function htmlConfigFrom(config: Record<string, unknown> | undefined): { html: string; css: string } {
  const html = config?.html
  const css = config?.css
  return {
    html: typeof html === 'string' ? html : '',
    css: typeof css === 'string' ? css : '',
  }
}
