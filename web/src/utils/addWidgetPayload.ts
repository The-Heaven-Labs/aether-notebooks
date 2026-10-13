export type AddWidgetSource = 'cell' | 'query' | 'none'

export type AddWidgetType = 'table' | 'chart' | 'metric' | 'text' | 'html' | 'custom'

export interface AddWidgetInput {
  source: AddWidgetSource
  type: AddWidgetType
  layout: { row: number; col: number; width: number; height: number; tab?: string }
  notebookId?: string
  cellId?: string
  connectorId?: string | null
  query?: string
  markdown?: string
}

/**
 * The widget types the picker may add for a given source. The source-less
 * option offers the html panel only when the org switch is on (htmlEnabled
 * defaults to true; the caller passes the resolved `html_panels_enabled`) and
 * the custom component only when custom code is enabled (default off).
 */
export function validTypesForSource(source: AddWidgetSource, htmlEnabled = true, customEnabled = false): AddWidgetType[] {
  if (source === 'none') {
    return ['text', ...(htmlEnabled ? ['html'] : []), ...(customEnabled ? ['custom'] : [])] as AddWidgetType[]
  }
  if (source === 'cell') return ['table', 'chart']
  return ['table', 'chart', 'metric']
}

/**
 * Builds the POST body for `/dashboards/{id}/widgets`. The backend allows
 * source-less widgets (no notebook/connector) — that is the text, html, or
 * custom component.
 */
export function buildAddWidgetPayload(input: AddWidgetInput): Record<string, unknown> {
  const base = { type: input.type, layout: input.layout, config: {} as Record<string, unknown> }
  if (input.source === 'none') {
    if (input.type === 'html' || input.type === 'custom') return { ...base, config: {} }
    return { ...base, type: 'text', config: { text: { markdown: input.markdown ?? '' } } }
  }
  if (input.source === 'query') {
    return { ...base, connector_id: input.connectorId, query: input.query, language: 'sql' }
  }
  return { ...base, notebook_id: input.notebookId, cell_id: input.cellId }
}
