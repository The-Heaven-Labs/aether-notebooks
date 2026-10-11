/**
 * Reads a text panel's markdown out of a widget config. Absent or malformed
 * `config.text.markdown` yields `''` so callers can render the empty hint.
 */
export function textConfigFrom(config: Record<string, unknown> | undefined): { markdown: string } {
  const text = config?.text
  if (text && typeof text === 'object') {
    const markdown = (text as Record<string, unknown>).markdown
    if (typeof markdown === 'string') return { markdown }
  }
  return { markdown: '' }
}
