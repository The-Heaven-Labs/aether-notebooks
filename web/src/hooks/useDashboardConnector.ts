import { useCallback, useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { effectiveAccess } from '../api/warehouses'
import type { Connector } from '../types'

function storageKey(userID: string, dashboardID: string) {
  return `aether_dash_connector:${userID}:${dashboardID}`
}

function readStored(key: string): string | null {
  try { return localStorage.getItem(key) } catch { return null }
}

interface Resolution {
  userID: string
  suggestion: string | null
}

/**
 * Per-viewer dashboard connector selection (the notebook pin's successor):
 * localStorage per user+dashboard. The viewer's warehouse preference is the
 * preselected default when the org resolves to exactly one live preference
 * (design 6.3 / D7's unambiguity rule); zero or several → no default.
 *
 * `GET /warehouses` is admin-only, so warehouses are derived from the
 * ClickHouse connectors the viewer can use (the ProfilePage routing pattern)
 * and each preference is read from the member-accessible effective-access
 * endpoint. A preference that no longer names a granted service is ignored.
 *
 * `resolving` stays true until the suggestion for the current user is known
 * (success or failure), so callers can hold widget queries back instead of
 * running them once on the saved connector and again on the default.
 */
export function useDashboardConnector(dashboardID: string, userID: string) {
  const queryClient = useQueryClient()
  const key = storageKey(userID, dashboardID)
  // Stored per user + dashboard so switching dashboards (or users on a shared
  // browser) re-reads the right value during render.
  const [choice, setChoice] = useState(() => ({ key, selected: readStored(key) }))
  if (choice.key !== key) {
    setChoice({ key, selected: readStored(key) })
  }
  const selected = choice.selected
  // The suggestion is derived from the resolution for the CURRENT user: while
  // a resolution is in flight (or after the user changed) the previous value
  // is never exposed.
  const [resolution, setResolution] = useState<Resolution | null>(null)
  const resolved = resolution?.userID === userID
  const resolving = !!userID && !resolved
  const suggestion = resolved ? resolution.suggestion : null

  useEffect(() => {
    if (!userID) return
    let cancelled = false
    ;(async () => {
      let next: string | null = null
      try {
        // Shares the ['connectors'] react-query cache with ConnectorSelector
        // so the page issues one list request, not two.
        const connectors = await queryClient.ensureQueryData<Connector[]>({
          queryKey: ['connectors'],
          queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
        })
        const warehouseIDs = new Set<string>()
        for (const c of connectors) {
          if (c.type !== 'clickhouse' || !c.warehouse_id || c.can_use === false) continue
          warehouseIDs.add(c.warehouse_id)
        }
        const preferences = await Promise.all(
          [...warehouseIDs].map(async (warehouseID) => {
            try {
              const access = await effectiveAccess(warehouseID)
              const id = access.preferred_connector_id
              return id && access.services.some(s => s.connector_id === id) ? id : null
            } catch {
              return null // warehouse unreadable — contributes no preference
            }
          }),
        )
        const live = preferences.filter((id): id is string => id != null)
        if (live.length === 1) next = live[0]
      } catch { /* connectors unreadable — no suggestion */ }
      if (!cancelled) setResolution({ userID, suggestion: next })
    })()
    return () => { cancelled = true }
  }, [userID, queryClient])

  const select = useCallback((connectorID: string | null) => {
    setChoice({ key, selected: connectorID })
    try {
      if (connectorID == null) localStorage.removeItem(key)
      else localStorage.setItem(key, connectorID)
    } catch { /* private mode */ }
  }, [key])

  return { selected, suggestion, select, resolving }
}
