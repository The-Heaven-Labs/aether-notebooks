export type AddWidgetSource = 'cell' | 'query' | 'none'

export type AddWidgetType = 'table' | 'chart' | 'metric' | 'text'

export interface AddWidgetInput {
  source: AddWidgetSource
  type: AddWidgetType
  layout: { row: number; col: number; width: number; height: number }
  notebookId?: string
  cellId?: string
  connectorId?: string | null
  query?: string
  markdown?: string
}

/** The widget types the picker may add for a given source. */
export function validTypesForSource(source: AddWidgetSource): AddWidgetType[] {
  if (source === 'none') return ['text']
  if (source === 'cell') return ['table', 'chart']
  return ['table', 'chart', 'metric']
}

/**
 * Builds the POST body for `/dashboards/{id}/widgets`. The backend allows
 * source-less widgets (no notebook/connector) — that is the text panel.
 */
export function buildAddWidgetPayload(input: AddWidgetInput): Record<string, unknown> {
  const base = { type: input.type, layout: input.layout, config: {} as Record<string, unknown> }
  if (input.source === 'none') {
    return { ...base, type: 'text', config: { text: { markdown: input.markdown ?? '' } } }
  }
  if (input.source === 'query') {
    return { ...base, connector_id: input.connectorId, query: input.query, language: 'sql' }
  }
  return { ...base, notebook_id: input.notebookId, cell_id: input.cellId }
}
