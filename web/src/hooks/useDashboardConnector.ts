import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import { effectiveAccess } from '../api/warehouses'
import type { Connector } from '../types'

function storageKey(userID: string, dashboardID: string) {
  return `aether_dash_connector:${userID}:${dashboardID}`
}

function readStored(key: string): string | null {
  try { return localStorage.getItem(key) } catch { return null }
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
 */
export function useDashboardConnector(dashboardID: string, userID: string) {
  const key = storageKey(userID, dashboardID)
  // Stored per user + dashboard so switching dashboards (or users on a shared
  // browser) re-reads the right value during render.
  const [choice, setChoice] = useState(() => ({ key, selected: readStored(key) }))
  if (choice.key !== key) {
    setChoice({ key, selected: readStored(key) })
  }
  const selected = choice.selected
  const [suggestion, setSuggestion] = useState<string | null>(null)

  useEffect(() => {
    if (!userID) return
    let cancelled = false
    ;(async () => {
      try {
        const connectors = await api.get<Connector[]>('/api/v1/connectors')
        const warehouseIDs = new Set<string>()
        for (const c of connectors) {
          if (c.type !== 'clickhouse' || !c.warehouse_id || c.can_use === false) continue
          warehouseIDs.add(c.warehouse_id)
        }
        const preferred: string[] = []
        for (const warehouseID of warehouseIDs) {
          try {
            const access = await effectiveAccess(warehouseID)
            const id = access.preferred_connector_id
            if (id && access.services.some(s => s.connector_id === id)) preferred.push(id)
          } catch { /* warehouse unreadable — contributes no preference */ }
        }
        if (!cancelled && preferred.length === 1) setSuggestion(preferred[0])
      } catch { /* connectors unreadable — no suggestion */ }
    })()
    return () => { cancelled = true }
  }, [userID])

  const select = useCallback((connectorID: string | null) => {
    setChoice({ key, selected: connectorID })
    try {
      if (connectorID == null) localStorage.removeItem(key)
      else localStorage.setItem(key, connectorID)
    } catch { /* private mode */ }
  }, [key])

  return { selected, suggestion, select }
}
