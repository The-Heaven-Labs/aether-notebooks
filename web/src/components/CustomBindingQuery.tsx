import { useEffect } from 'react'
import { useWidgetQuery } from '../hooks/useWidgetQuery'
import type { Widget } from '../types'

export interface BindingResult { key: string; status: 'ready' | 'unavailable'; result?: unknown; message?: string }

/**
 * Resolves one custom-component binding for the host bridge:
 *  - query widgets execute through the dashboard execute pipeline (draft,
 *    published, or the public endpoint — the caller passes the base);
 *  - cell widgets resolve their stored outputs (never executed here);
 *  - anything unresolved reports `unavailable`, which the host forwards as an
 *    error frame (the component shows its own empty state).
 */
export function CustomBindingQuery({ bindingKey, widget, values, viewerConnectorId, endpointBase, published, cellOutputs, onResult }: {
  bindingKey: string
  widget: Widget | undefined
  values: Record<string, unknown>
  viewerConnectorId?: string | null
  endpointBase?: string
  published?: boolean
  cellOutputs?: unknown[] | null
  onResult: (result: BindingResult) => void
}) {
  const isQuery = !!widget?.connector_id && !!widget?.query
  const query = useWidgetQuery({
    dashboardId: '',
    widget: widget ?? ({ id: 'missing' } as Widget),
    values,
    enabled: isQuery && !!endpointBase,
    viewerConnectorId,
    endpointBase,
    published,
  })

  useEffect(() => {
    if (!widget) {
      onResult({ key: bindingKey, status: 'unavailable', message: `binding "${bindingKey}" points at a missing widget` })
      return
    }
    if (isQuery) {
      if (query.isLoading) return
      if (query.error) {
        onResult({ key: bindingKey, status: 'unavailable', message: query.error instanceof Error ? query.error.message : 'query failed' })
        return
      }
      if (query.data) onResult({ key: bindingKey, status: 'ready', result: query.data })
      return
    }
    onResult({ key: bindingKey, status: 'ready', result: { outputs: cellOutputs ?? [] } })
  }, [bindingKey, widget, isQuery, query.isLoading, query.error, query.data, cellOutputs, onResult])

  return null
}
