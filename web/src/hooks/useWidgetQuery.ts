import { useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Widget, WidgetQueryResult } from '../types'

function canonicalValues(values: Record<string, unknown>): string {
  return JSON.stringify(Object.keys(values).sort().map(k => [k, values[k]]))
}

export function useWidgetQuery(opts: {
  dashboardId: string
  widget: Widget
  values: Record<string, unknown>
  enabled: boolean
  /** Overrides the execute endpoint base, e.g. `/api/v1/public/{token}` for public dashboards. */
  endpointBase?: string
}) {
  const bypassRef = useRef(false)
  const endpointBase = opts.endpointBase ?? `/api/v1/dashboards/${opts.dashboardId}`
  const query = useQuery({
    queryKey: ['widget-query', endpointBase, opts.widget.id, canonicalValues(opts.values)],
    queryFn: ({ signal }) => {
      const bypass = bypassRef.current
      bypassRef.current = false
      return api.post<WidgetQueryResult>(
        `${endpointBase}/execute`,
        { widget_id: opts.widget.id, variables: opts.values, bypass_cache: bypass },
        { signal },
      )
    },
    enabled: opts.enabled && !!opts.widget.connector_id && !!opts.widget.query,
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
  })
  const refresh = () => { bypassRef.current = true; void query.refetch() }
  return { ...query, refresh }
}
