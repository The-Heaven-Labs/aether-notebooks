import { useCallback, useRef } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
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
  /** Viewer's dashboard connector selection, forwarded to widget execution. */
  viewerConnectorId?: string | null
  /** Overrides the execute endpoint base, e.g. `/api/v1/public/{token}` for public dashboards. */
  endpointBase?: string
  /**
   * Resolve the widget definition from the published snapshot server-side.
   * Omitted from the body when false; the draft and published results live in
   * separate cache entries so a mode switch can never serve the other's rows.
   */
  published?: boolean
}) {
  const bypassRef = useRef(false)
  const endpointBase = opts.endpointBase ?? `/api/v1/dashboards/${opts.dashboardId}`
  const published = opts.published ?? false
  const query = useQuery({
    queryKey: ['widget-query', endpointBase, opts.widget.id, opts.viewerConnectorId ?? '', published ? 'published' : 'draft', canonicalValues(opts.values)],
    queryFn: ({ signal }) => {
      const bypass = bypassRef.current
      bypassRef.current = false
      return api.post<WidgetQueryResult>(
        `${endpointBase}/execute`,
        {
          widget_id: opts.widget.id,
          connector_id: opts.viewerConnectorId ?? undefined,
          variables: opts.values,
          bypass_cache: bypass,
          // Only a published run claims the mode; the draft omits the key so
          // the server resolves the live definitions.
          ...(published ? { published: true } : {}),
        },
        { signal },
      )
    },
    enabled: opts.enabled && !!opts.widget.connector_id && !!opts.widget.query,
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
    // Keep the previous frame on screen while a new filter combination loads:
    // the widget dims instead of collapsing into a "Loading…" jump.
    placeholderData: keepPreviousData,
  })
  const refetch = query.refetch
  const refresh = useCallback(async () => {
    bypassRef.current = true
    return refetch()
  }, [refetch])
  // Cache-eligible re-run: does not set the bypass flag, so the server serves
  // (or refreshes) the shared cache entry. Live definition changes use this;
  // only the explicit Refresh action bypasses the cache.
  const rerun = useCallback(() => refetch(), [refetch])
  return { ...query, refresh, rerun }
}
