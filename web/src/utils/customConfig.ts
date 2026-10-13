export interface CustomBinding { key: string; widget_id: string }

/** Reads a custom component's bundle; malformed bindings entries are dropped. */
export function customConfigFrom(config: Record<string, unknown> | undefined): {
  html: string; css: string; js: string; bindings: CustomBinding[]
} {
  const html = typeof config?.html === 'string' ? config.html : ''
  const css = typeof config?.css === 'string' ? config.css : ''
  const js = typeof config?.js === 'string' ? config.js : ''
  const bindings: CustomBinding[] = []
  if (Array.isArray(config?.bindings)) {
    for (const entry of config.bindings) {
      if (!entry || typeof entry !== 'object') continue
      const key = (entry as Record<string, unknown>).key
      const widgetID = (entry as Record<string, unknown>).widget_id
      if (typeof key === 'string' && key && typeof widgetID === 'string' && widgetID) {
        bindings.push({ key, widget_id: widgetID })
      }
    }
  }
  return { html, css, js, bindings }
}
