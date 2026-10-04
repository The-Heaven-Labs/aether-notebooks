import { useCallback, useEffect, useRef, useState } from 'react'
import { Play, Loader2, RefreshCw } from 'lucide-react'
import { ApiError } from '../api/client'
import { serviceChoicesFromError, setPreference } from '../api/warehouses'
import type { ServiceChoicePrompt } from '../api/warehouses'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { useWidgetQuery } from '../hooks/useWidgetQuery'
import { OutputRenderer } from './OutputRenderer'
import { ServiceChoiceDialog } from './RoutingPreference'
import { normalizeChartConfig } from '../charts/normalizeChartConfig'
import type { Widget } from '../types'

const styles: Record<string, React.CSSProperties> = {
  root: {
    flex: 1,
    minHeight: 0,
    display: 'flex',
    flexDirection: 'column',
  },
  muted: { padding: '16px', fontSize: 13, color: 'var(--text-muted)' },
  empty: { padding: '16px', fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  error: { padding: '16px', fontSize: 13, color: 'var(--danger, #d33)', display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' },
  retry: {
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontSize: 12,
    padding: '3px 10px',
  },
  footer: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    flex: 1,
  },
  footerBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 20,
    height: 20,
    borderRadius: 3,
    border: '1px solid var(--border)',
    background: 'var(--bg-card)',
    color: 'var(--text-muted)',
    cursor: 'pointer',
    padding: 0,
  },
  footerText: {
    marginLeft: 'auto',
    fontSize: 10,
    color: 'var(--text-muted)',
    opacity: 0.6,
    whiteSpace: 'nowrap',
  },
}

export function QueryDataWidget({ dashboardId, widget, canViewWithData, refreshNonce = 0, endpointBase }: {
  dashboardId: string
  widget: Widget
  canViewWithData: boolean
  refreshNonce?: number
  /** Overrides the execute endpoint base, e.g. `/api/v1/public/{token}` for public dashboards. */
  endpointBase?: string
}) {
  const isPublic = !!endpointBase
  const { values } = useDashboardVariables()
  const { data, error, isPending, isFetching, refresh } = useWidgetQuery({
    dashboardId,
    widget,
    values,
    enabled: canViewWithData,
    endpointBase,
  })
  const [fetchedAt, setFetchedAt] = useState<Date | null>(null)
  const [serviceChoice, setServiceChoice] = useState<ServiceChoicePrompt | null>(null)
  const [serviceChoiceError, setServiceChoiceError] = useState<string | null>(null)
  const [serviceChoiceSaving, setServiceChoiceSaving] = useState(false)

  useEffect(() => {
    if (data) setFetchedAt(new Date())
  }, [data])

  // Run-all and auto-refresh bump the nonce; skip the initial mount so a
  // widget doesn't double-fetch right after its first render.
  const refreshRef = useRef(refresh)
  refreshRef.current = refresh
  const lastNonce = useRef(refreshNonce)
  useEffect(() => {
    if (refreshNonce !== lastNonce.current) {
      lastNonce.current = refreshNonce
      refreshRef.current()
    }
  }, [refreshNonce])

  useEffect(() => {
    const prompt = serviceChoicesFromError(error)
    // Public visitors cannot store a routing preference; surface an
    // explanatory error instead of a dialog that would fail to save.
    if (prompt && !isPublic) setServiceChoice(prompt)
  }, [error, isPublic])

  const chooseService = useCallback(async (connectorId: string) => {
    const warehouseId = serviceChoice?.warehouseId
    if (!warehouseId) {
      setServiceChoiceError('Could not determine which warehouse this connector belongs to.')
      return
    }
    setServiceChoiceSaving(true)
    setServiceChoiceError(null)
    try {
      await setPreference(warehouseId, connectorId)
      setServiceChoice(null)
      refreshRef.current()
    } catch (e) {
      setServiceChoiceError(e instanceof Error ? e.message : 'Failed to save preference')
    } finally {
      setServiceChoiceSaving(false)
    }
  }, [serviceChoice])

  const dialog = (
    <ServiceChoiceDialog
      open={!!serviceChoice}
      services={serviceChoice?.services ?? []}
      saving={serviceChoiceSaving}
      error={serviceChoiceError}
      onSelect={chooseService}
      onCancel={() => { setServiceChoice(null); setServiceChoiceError(null) }}
      onDismissError={() => setServiceChoiceError(null)}
    />
  )

  if (!canViewWithData) {
    return <div style={styles.muted}>You need view_with_data access to see data for this widget.</div>
  }

  if (isPending && !error) {
    return <div style={styles.muted}>Loading…</div>
  }

  if (error) {
    if (serviceChoicesFromError(error)) {
      if (isPublic) {
        return (
          <div style={styles.muted}>
            This widget needs a warehouse service preference. Ask the dashboard owner to set one.
          </div>
        )
      }
      return (
        <>
          <div style={styles.muted}>Choose a warehouse service to run this widget.</div>
          {dialog}
        </>
      )
    }
    const forbidden = error instanceof ApiError && error.status === 403
    return (
      <div style={styles.error}>
        <span>{error instanceof Error ? error.message : 'Query failed'}</span>
        {!forbidden && (
          <button style={styles.retry} onClick={() => refreshRef.current()}>Retry</button>
        )}
        {dialog}
      </div>
    )
  }

  const outputs = data?.outputs ?? []
  if (!outputs.length) {
    return (
      <div style={styles.empty}>
        No data returned.{' '}
        <button style={styles.retry} onClick={() => refreshRef.current()}>Run</button>
      </div>
    )
  }

  const footerExtra = (
    <div style={styles.footer}>
      <button
        style={styles.footerBtn}
        onClick={() => refreshRef.current()}
        disabled={isFetching}
        title="Refresh widget data"
      >
        {isFetching ? <Loader2 size={10} style={{ animation: 'spin 1s linear infinite' }} /> : <Play size={10} />}
      </button>
      {fetchedAt && (
        <span style={styles.footerText}>
          {/* Same date + time format as the cell widgets' "Executed at". */}
          Executed at {fetchedAt.toLocaleDateString([], { year: 'numeric', month: '2-digit', day: '2-digit' })} {fetchedAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
          {data?.cached && <span> · cached</span>}
          {data?.metrics?.query_time_ms != null && <span> · {data.metrics.query_time_ms}ms</span>}
        </span>
      )}
      {data?.cached && <RefreshCw size={10} style={{ color: 'var(--text-muted)' }} />}
    </div>
  )

  return (
    <div style={styles.root}>
      <OutputRenderer
        outputs={outputs}
        fixedView={widget.type === 'chart' ? 'chart' : 'table'}
        chartConfig={normalizeChartConfig(widget.config)}
        footerExtra={footerExtra}
      />
      {dialog}
    </div>
  )
}
