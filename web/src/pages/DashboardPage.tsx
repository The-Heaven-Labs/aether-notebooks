import { useEffect, useRef, useState, useCallback, useLayoutEffect, useMemo } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft, Loader2, Pencil, Settings, Globe, RefreshCw } from 'lucide-react'
import { ShareModal } from '../components/ShareModal'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard, Notebook, Cell, Widget } from '../types'
import type { ChartConfig } from '../charts/types'
import { mergeWidgetChartConfig, hasWidgetOverride, withWidgetOverride } from '../charts/widgetChartConfig'
import { formatExecutedAt } from '../utils/formatDateTime'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { OutputRenderer } from '../components/OutputRenderer'
import { CollaboratorAvatars } from '../components/CollaboratorAvatars'
import { DashboardVariablesProvider } from '../contexts/DashboardVariablesContext'
import { DashboardVariableBar } from '../components/DashboardVariableBar'
import { rescaleWidgetLayouts } from '../utils/dashboardGrid'
import { QueryDataWidget } from '../components/QueryDataWidget'
import { ConnectorSelector } from '../components/ConnectorSelector'
import { useDashboardConnector } from '../hooks/useDashboardConnector'
import { useDashboardDoc } from '../hooks/useDashboardDoc'
import { useAuth } from '../hooks/useAuth'
import { GridLayout } from 'react-grid-layout'
import type { LayoutItem } from 'react-grid-layout'
import 'react-grid-layout/css/styles.css'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

interface DashboardWithWidgets extends Dashboard {
  widgets: Widget[]
  can_share?: boolean
  can_edit?: boolean
}

// Widget type extended with input widget variants (config is typed loosely)
type AnyWidget = Widget & { type: string; config: any }

function isQueryWidget(w: AnyWidget): w is AnyWidget & { connector_id: string; query: string } {
  return !!w.connector_id && !!w.query
}

function CellDataWidget({ widget, qc, widgetsData, dashboardId, loading, onRun, onEdit }: { widget: AnyWidget; qc: ReturnType<typeof useQueryClient>; widgetsData?: DashboardWithWidgets['widgets_data']; dashboardId?: string; loading?: boolean; onRun?: () => void; onEdit?: () => void }) {
  const handleChartConfigChange = useCallback((config: ChartConfig) => {
    if (!dashboardId) return
    api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, {
      config: withWidgetOverride(config),
    }).then(() => {
      qc.invalidateQueries({ queryKey: ['dashboard', dashboardId] })
    })
  }, [dashboardId, widget.id, qc])

  const handleChartConfigReset = useCallback(() => {
    if (!dashboardId) return
    api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, { config: {} }).then(() => {
      qc.invalidateQueries({ queryKey: ['dashboard', dashboardId] })
    })
  }, [dashboardId, widget.id, qc])

  // Use widgets_data when available (view_with_data permission) instead of fetching notebooks individually
  const widgetCellData = widgetsData?.[widget.cell_id!]
  const useInlinedData = !!widgetCellData

  const { data: notebook, isLoading } = useQuery({
    queryKey: ['notebook', widget.notebook_id],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${widget.notebook_id}`),
    enabled: !useInlinedData,
  })

  if (!useInlinedData && isLoading) return <div style={queryWidgetStyles.loading}>Loading…</div>

  const cell = useInlinedData
    ? widgetCellData!
    : notebook?.cells?.find((c: Cell) => c.id === widget.cell_id)
  if (!cell) return <div style={queryWidgetStyles.empty}>Cell not found</div>

  // Markdown cells render their source directly — they don't need to be "run"
  if (cell.type === 'text') {
    return (
      <div style={queryWidgetStyles.markdown}>
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{cell.source || ''}</ReactMarkdown>
      </div>
    )
  }

  if (!cell.outputs?.length) {
    return (
      <div style={queryWidgetStyles.empty}>
        No data yet.{' '}
        <button
          style={{ color: 'var(--accent)', background: 'none', border: 'none', cursor: 'pointer', fontSize: 13, padding: 0 }}
          onClick={() => {
            const token = localStorage.getItem('aether_token')
            fetch(`/api/v1/notebooks/${widget.notebook_id}/cells/${widget.cell_id}/execute`, {
              method: 'POST',
              headers: {
                'Content-Type': 'application/json',
                ...(token ? { Authorization: `Bearer ${token}` } : {}),
              },
              body: JSON.stringify({ parameters: {} }),
            }).then(() => {
              if (useInlinedData && dashboardId) {
                qc.invalidateQueries({ queryKey: ['dashboard', dashboardId] })
              } else {
                qc.invalidateQueries({ queryKey: ['notebook', widget.notebook_id] })
              }
            })
          }}
        >
          Run cell
        </button>
      </div>
    )
  }
  const fixedView = widget.type === 'chart' ? 'chart' : 'table'
  const chartConfig = mergeWidgetChartConfig((cell as any).metadata?.chart, widget.config)
  const chartOverridden = hasWidgetOverride(widget.config)
  const updatedAt = (cell as any).updated_at
  const durationMs = (cell as any).duration_ms
  const widgetLabel = (() => {
    const cfg = widget.config as Record<string, unknown> | undefined
    const t = typeof cfg?.title === 'string' ? cfg.title : typeof cfg?.label === 'string' ? cfg.label : ''
    return t || null
  })()
  const footerExtra = (
    <div style={{ display: 'flex', alignItems: 'center', gap: 6, flex: 1, minWidth: 0 }}>
      {onRun && (
        <button
          style={{
            display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
            width: 20, height: 20, borderRadius: 3,
            border: '1px solid var(--border)', background: 'var(--bg-card)',
            color: 'var(--text-muted)', cursor: 'pointer', padding: 0,
          }}
          onClick={onRun}
          disabled={loading}
          title={widgetLabel ? `Refresh ${widgetLabel}` : 'Refresh widget data'}
          aria-label={widgetLabel ? `Refresh ${widgetLabel}` : 'Refresh widget data'}
        >
          {loading ? <Loader2 size={10} style={{ animation: 'spin 1s linear infinite' }} /> : <RefreshCw size={10} />}
        </button>
      )}
      {onEdit && (
        <button
          style={{
            display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
            width: 20, height: 20, borderRadius: 3,
            border: '1px solid var(--border)', background: 'var(--bg-card)',
            color: 'var(--text-muted)', cursor: 'pointer', padding: 0,
          }}
          onClick={onEdit}
          title={widgetLabel ? `Edit ${widgetLabel}` : 'Edit widget'}
          aria-label={widgetLabel ? `Edit ${widgetLabel}` : 'Edit widget'}
        >
          <Pencil size={9} />
        </button>
      )}
      {updatedAt && (
        <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--text-muted)', whiteSpace: 'nowrap', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>
          Executed at {formatExecutedAt(updatedAt)}
          {durationMs != null && <span> · {durationMs}ms</span>}
        </span>
      )}
    </div>
  )
  return (
    <>
      <OutputRenderer outputs={cell.outputs} fixedView={fixedView} chartConfig={chartConfig} onChartConfigChange={handleChartConfigChange} chartConfigOverridden={chartOverridden} onChartConfigReset={handleChartConfigReset} footerExtra={footerExtra} />
    </>
  )
}

const queryWidgetStyles: Record<string, React.CSSProperties> = {
  loading: { padding: '16px', fontSize: 13, color: 'var(--text-muted)' },
  empty: { padding: '16px', fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  markdown: { padding: '16px', fontSize: 14, color: 'var(--text-primary)', lineHeight: 1.6, overflow: 'auto', height: '100%' },
}

function WidgetCard({ widget, qc, widgetsData, dashboardId, onEdit, onFetchingChange }: { widget: AnyWidget; qc: ReturnType<typeof useQueryClient>; widgetsData?: DashboardWithWidgets['widgets_data']; dashboardId?: string; onEdit?: () => void; onFetchingChange?: (fetching: boolean) => void }) {
  const [loading, setLoading] = useState(false)

  // Report cell-widget runs so the header can show a single progress signal.
  const onFetchingRef = useRef(onFetchingChange)
  onFetchingRef.current = onFetchingChange
  useEffect(() => { onFetchingRef.current?.(loading) }, [loading])
  useEffect(() => () => onFetchingRef.current?.(false), [])

  const handleRun = useCallback(async () => {
    if (loading || !widget.notebook_id || !widget.cell_id) return
    setLoading(true)
    try {
      const token = localStorage.getItem('aether_token')
      await fetch(`/api/v1/notebooks/${widget.notebook_id}/cells/${widget.cell_id}/execute`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({ parameters: {} }),
      })
      qc.invalidateQueries({ queryKey: ['notebook', widget.notebook_id] })
    } finally {
      setLoading(false)
    }
  }, [widget.notebook_id, widget.cell_id, qc, loading])

  return (
    <div style={styles.widgetCard}>
      <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
        <CellDataWidget widget={widget} qc={qc} widgetsData={widgetsData} dashboardId={dashboardId} loading={loading} onRun={handleRun} onEdit={onEdit} />
      </div>
    </div>
  )
}

const toGridItem = (w: Widget): LayoutItem => ({
  i: w.id,
  x: w.layout.col,
  y: w.layout.row,
  w: w.layout.width,
  // Clamp the rendered height too: minH only constrains resizing, so a
  // 4-row widget would still render ~40px of chart under the widget chrome.
  h: Math.max(w.layout.height, 6),
  minW: 2,
  minH: 6,
  maxH: 24,
})

function DashboardContent({ id }: { id: string }) {
  const qc = useQueryClient()
  const { user } = useAuth()
  const userEmail = localStorage.getItem('aether_user_email') ?? ''
  const { selected, suggestion, select, resolving } = useDashboardConnector(id, user?.user_id ?? '')
  // The live default (viewer's sole warehouse preference) is preselected;
  // an explicit pick persists and overrides it.
  const viewerConnectorId = selected ?? suggestion
  const [containerWidth, setContainerWidth] = useState(0)
  const [refreshSeconds, setRefreshSeconds] = useState<number>(0)
  const [refreshCustom, setRefreshCustom] = useState(false)
  const [showShare, setShowShare] = useState(false)
  const [isRunningAll, setIsRunningAll] = useState(false)
  const gridContainerRef = useRef<HTMLDivElement | null>(null)

  // Observe the grid as soon as it mounts (a mount-time effect would run
  // while the loading skeleton is up and never attach), so window resizes
  // reflow the layout without a reload.
  const gridObserverRef = useRef<ResizeObserver | null>(null)
  const gridRef = useCallback((el: HTMLDivElement | null) => {
    gridContainerRef.current = el
    gridObserverRef.current?.disconnect()
    gridObserverRef.current = null
    if (el) {
      setContainerWidth(el.clientWidth)
      const obs = new ResizeObserver(([entry]) => {
        setContainerWidth(entry.contentRect.width)
      })
      obs.observe(el)
      gridObserverRef.current = obs
    }
  }, [])

  useLayoutEffect(() => {
    const el = gridContainerRef.current
    if (!el) return
    const w = el.clientWidth
    if (w !== containerWidth) {
      setContainerWidth(w)
    }
  })

  const { data: dashboard, isLoading, error } = useQuery({
    queryKey: ['dashboard', id],
    queryFn: () => api.get<DashboardWithWidgets>(`/api/v1/dashboards/${id}`),
    enabled: !!id,
  })

  // Live dashboard document: connects as soon as the page mounts and becomes
  // the source of truth for title/settings/widgets once synced. The viewer is
  // read-only (mutators stay disabled); when the relay is unreachable the
  // document never syncs and the REST snapshot keeps painting the page.
  const liveDoc = useDashboardDoc(id)
  const liveTitle = liveDoc.synced && liveDoc.title ? liveDoc.title : (dashboard?.title ?? '')

  // Per-widget fetch state, reported by the widgets themselves, so the header
  // can show one honest "Refreshing n/m" signal instead of a button that goes
  // idle before the widgets actually finish.
  const [fetchingWidgets, setFetchingWidgets] = useState<Record<string, boolean>>({})
  const [justUpdated, setJustUpdated] = useState(false)
  const refreshersRef = useRef(new Map<string, () => Promise<unknown>>())
  const wasRefreshing = useRef(false)

  const reportFetching = useCallback((widgetId: string, fetching: boolean) => {
    setFetchingWidgets(prev => (!!prev[widgetId] === fetching ? prev : { ...prev, [widgetId]: fetching }))
  }, [])
  const registerWidgetRefresh = useCallback((widgetId: string, fn: () => Promise<unknown>) => {
    refreshersRef.current.set(widgetId, fn)
    return () => { refreshersRef.current.delete(widgetId) }
  }, [])

  const refreshingCount = useMemo(
    () => Object.values(fetchingWidgets).filter(Boolean).length,
    [fetchingWidgets],
  )

  useEffect(() => {
    const was = wasRefreshing.current
    wasRefreshing.current = refreshingCount > 0
    if (was && refreshingCount === 0) {
      setJustUpdated(true)
      const t = setTimeout(() => setJustUpdated(false), 2500)
      return () => clearTimeout(t)
    }
  }, [refreshingCount])

  useEffect(() => {
    if (dashboard) {
      document.title = liveTitle ? `${liveTitle} — Aether Notebooks` : 'Aether Notebooks'
      const el = gridContainerRef.current
      if (el) setContainerWidth(el.clientWidth)
    }
    return () => { document.title = "Aether Notebooks" }
  }, [dashboard, liveTitle])

  async function executeAllWidgets(widgetList: AnyWidget[]) {
    if (isRunningAll) return
    const token = localStorage.getItem('aether_token')
    const cellWidgets = widgetList.filter(w => !isQueryWidget(w) && w.notebook_id && w.cell_id)
    const queryWidgets = widgetList.filter(isQueryWidget)
    if (!cellWidgets.length && !queryWidgets.length) return
    setIsRunningAll(true)
    try {
      // Query widgets refresh through their registered (cache-bypassing)
      // refreshers; awaiting them means the batch is actually done when the
      // button resets, instead of a nonce bump that finishes before the data.
      const queryRuns = queryWidgets
        .map(w => refreshersRef.current.get(w.id)?.())
        .filter((p): p is Promise<unknown> => !!p)
      await Promise.allSettled([
        ...cellWidgets.map(w =>
          fetch(`/api/v1/notebooks/${w.notebook_id}/cells/${w.cell_id}/execute`, {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              ...(token ? { Authorization: `Bearer ${token}` } : {}),
            },
            body: JSON.stringify({ parameters: {} }),
          }),
        ),
        ...queryRuns,
      ])
      if (dashboard?.can_view_with_data) {
        qc.invalidateQueries({ queryKey: ['dashboard', id] })
      } else {
        const notebookIds = [...new Set(cellWidgets.map(w => w.notebook_id).filter(Boolean))]
        notebookIds.forEach(nbId => qc.invalidateQueries({ queryKey: ['notebook', nbId] }))
      }
    } finally {
      setIsRunningAll(false)
    }
  }

  // Once the document has synced it is the source of truth for the widget
  // list; until then the REST payload paints the page. Timestamps are not
  // document fields, so doc widgets inherit `created_at` from their REST rows.
  const widgets = useMemo<AnyWidget[]>(() => {
    const rest = dashboard?.widgets ?? []
    if (!liveDoc.synced) return rest as AnyWidget[]
    const restById = new Map(rest.map(w => [w.id, w]))
    return liveDoc.widgets.map(w => {
      const restWidget = restById.get(w.id)
      return (restWidget?.created_at ? { ...w, created_at: restWidget.created_at } : w) as AnyWidget
    })
  }, [liveDoc.synced, liveDoc.widgets, dashboard?.widgets])

  // Live-merged settings: the document is authoritative for title, settings,
  // and variables once synced. `can_*` flags and cell outputs (`widgets_data`)
  // are not document fields and stay on the REST row.
  const settings = useMemo(
    () => (liveDoc.synced
      ? { ...(dashboard?.settings ?? {}), ...liveDoc.settings, variables: liveDoc.variables }
      : dashboard?.settings),
    [liveDoc.synced, liveDoc.settings, liveDoc.variables, dashboard?.settings],
  )

  // Grid density and the refresh cadence are persisted dashboard settings, so
  // they are edit actions — viewers don't get to restyle the shared board.
  const canEdit = dashboard?.can_edit === true
  const autoRefreshSecs = settings?.auto_refresh_seconds ?? 0
  const PRESET_REFRESH = [0, 30, 60, 300, 600]
  const customRefreshText = refreshCustom && refreshSeconds > 0 ? ` — ${refreshSeconds}s` : ''

  // Sync local state from dashboard data
  useEffect(() => {
    setRefreshSeconds(autoRefreshSecs)
    setRefreshCustom(!PRESET_REFRESH.includes(autoRefreshSecs))
  }, [autoRefreshSecs])

  useEffect(() => {
    if (!refreshSeconds || refreshSeconds <= 0 || !widgets.length) return
    const intervalId = setInterval(() => executeAllWidgets(widgets), refreshSeconds * 1000)
    return () => clearInterval(intervalId)
  }, [refreshSeconds, widgets])

  if (isLoading) {
    return (
      <div style={styles.loadingPage}>
        <div style={styles.loadingDot} />
      </div>
    )
  }

  if (error || !dashboard) {
    return (
      <div style={styles.loadingPage}>
        <p style={{ color: 'var(--text-secondary)' }}>
          {error ? (error as Error).message : 'Dashboard not found'}
        </p>
      </div>
    )
  }

  const variables = settings?.variables ?? []

  return (
    <DashboardVariablesProvider dashboardId={dashboard.id} variables={variables} viewerConnectorId={viewerConnectorId}>
    <AppShell noPadding>
      {/* Sub-header */}
      <header className="dash-header" style={styles.subHeader}>
        <div className="dash-header-left" style={styles.headerLeft}>
          <Link to="/dashboards" style={styles.backLink}>
            <ArrowLeft size={14} style={{ flexShrink: 0 }} />
            <span>Dashboards</span>
          </Link>
          <span style={styles.breadcrumbSep}>/</span>
          <span className="dash-title" style={styles.dashboardTitle}>{liveTitle}</span>
        </div>
        {/* Run all + auto-refresh */}
        <div className="dash-header-right" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {/* Presence + live state: avatars come from the document's awareness
              and the indicator lights while the relay is connected. When the
              relay is unavailable nothing renders here and the page keeps
              serving the REST snapshot. */}
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
            <CollaboratorAvatars
              awareness={liveDoc.awareness}
              currentUserEmail={userEmail}
            />
            {liveDoc.connected && (
              <span style={styles.liveIndicator} title="Live updates from collaborators">
                <span style={styles.liveDot} /> Live
              </span>
            )}
          </span>
          {/* Per-viewer connector selection: query widgets in the same
              warehouse run on the selected service; others keep their own.
              allowClear is off while a live default exists — clearing would
              just re-select the suggestion on the next render. */}
          <span
            title="Choose which connector this dashboard's queries run on for you"
            style={{ display: 'inline-flex' }}
          >
            <ConnectorSelector
              value={viewerConnectorId}
              onChange={select}
              types={['clickhouse']}
              allowClear={!suggestion}
              placeholder="Run widgets on…"
              style={{
                fontSize: 12, maxWidth: 220,
                background: 'rgba(255,255,255,0.06)',
                color: 'var(--nav-text)',
                borderColor: 'var(--nav-border)',
              }}
            />
          </span>
          {/* One honest progress signal for every widget refresh: counts while
              work is in flight, then a brief confirmation. */}
          <span
            role="status"
            aria-live="polite"
            className="nav-status"
            style={{
              fontSize: 10, fontFamily: 'var(--font-mono)',
              minWidth: 96, textAlign: 'right', whiteSpace: 'nowrap',
            }}
          >
            {refreshingCount > 0
              ? `Refreshing ${refreshingCount}/${widgets.length}…`
              : justUpdated ? 'Updated' : ''}
          </span>
          <button
            style={{
              padding: '5px 12px', fontSize: 12, fontWeight: 600,
              background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)',
              border: 'none', borderRadius: 4, cursor: 'pointer',
              display: 'inline-flex', alignItems: 'center', gap: 5,
              opacity: isRunningAll || refreshingCount > 0 ? 0.6 : 1,
            }}
            disabled={isRunningAll || refreshingCount > 0}
            onClick={() => executeAllWidgets(widgets)}
            title="Re-run every widget's query with the current filters"
          >
            <RefreshCw size={12} style={isRunningAll || refreshingCount > 0 ? { animation: 'spin 1s linear infinite' } : undefined} />
            {isRunningAll || refreshingCount > 0 ? 'Refreshing…' : 'Refresh'}
          </button>
          <Link to={`/dashboards/${id}`} className="nav-btn" title="Edit dashboard layout">
            <Settings size={12} /> Edit
          </Link>

          {dashboard?.can_share !== false && (
            <button
              type="button"
              className="nav-btn"
              onClick={() => setShowShare(true)}
              title="Share dashboard"
            >
              <Globe size={12} /> Share
            </button>
          )}

          {/* Column count selector (edit-permission only: it persists settings) */}
          {canEdit && <div className="dash-cols nav-seg">
            {[6, 8, 12, 16, 24].map(cols => (
              <button
                key={cols}
                type="button"
                aria-pressed={(settings?.grid_cols ?? 12) === cols}
                onClick={async () => {
                  const oldCols = settings?.grid_cols ?? 12
                  await api.put(`/api/v1/dashboards/${id}`, {
                    settings: { ...(settings ?? {}), grid_cols: cols },
                  })
                  if (oldCols !== cols) {
                    // Reflow widget layouts so nothing falls outside the new
                    // grid (a bare column change used to push widgets off-view).
                    const updates = rescaleWidgetLayouts(widgets as unknown as Widget[], oldCols, cols)
                    await Promise.allSettled(
                      updates.map(u => api.put(`/api/v1/dashboards/${id}/widgets/${u.id}`, { layout: u.layout })),
                    )
                  }
                  qc.invalidateQueries({ queryKey: ['dashboard', id] })
                }}
                title={`${cols} columns`}
              >
                {cols}
              </button>
            ))}
          </div>}

          {canEdit && <div style={{ position: 'relative' }}>
            <select
              className="nav-select"
              style={{
                fontSize: 12, padding: '4px 24px 4px 8px',
                cursor: 'pointer', appearance: 'none', WebkitAppearance: 'none', MozAppearance: 'none',
              }}
              value={refreshCustom ? 'custom' : String(refreshSeconds)}
              onChange={async e => {
                const val = e.target.value
                if (val === 'custom') { setRefreshCustom(true); return }
                setRefreshCustom(false)
                const secs = parseInt(val)
                setRefreshSeconds(secs)
                if (secs === autoRefreshSecs) return
                await api.put(`/api/v1/dashboards/${id}`, {
                  settings: { ...(settings ?? {}), auto_refresh_seconds: secs },
                })
                qc.invalidateQueries({ queryKey: ['dashboard', id] })
              }}
              title="Auto-refresh interval"
              aria-label="Auto-refresh interval"
            >
              <option value="0">No auto-refresh</option>
              <option value="30">Every 30s</option>
              <option value="60">Every 1m</option>
              <option value="300">Every 5m</option>
              <option value="600">Every 10m</option>
              <option value="custom">Custom{customRefreshText}</option>
            </select>
            <svg viewBox="0 0 10 6" width="10" height="6" style={{ position: 'absolute', right: 8, top: '50%', transform: 'translateY(-50%)', pointerEvents: 'none', fill: 'none', stroke: 'currentColor', strokeWidth: 1.5 }}>
              <polyline points="1,1 5,5 9,1" />
            </svg>
          </div>}
          {canEdit && refreshCustom && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginLeft: 4 }}>
              <input
                type="text"
               
               
                className="nav-input"
                style={{ width: 72, fontSize: 12, padding: '4px 6px' }}
                value={refreshSeconds}
                onChange={e => {
                  const val = parseInt(e.target.value)
                  if (!isNaN(val) && val >= 0) setRefreshSeconds(val)
                }}
                onBlur={async () => {
                  if (refreshSeconds === autoRefreshSecs) return
                  await api.put(`/api/v1/dashboards/${id}`, {
                    settings: { ...(settings ?? {}), auto_refresh_seconds: refreshSeconds },
                  })
                  qc.invalidateQueries({ queryKey: ['dashboard', id] })
                }}
                onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur() }}
                title="Custom refresh interval in seconds (0 = off)"
                aria-label="Custom refresh interval in seconds"
              />
              <span style={{ fontSize: 11, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>s</span>
            </div>
          )}
        </div>
      </header>

      <div style={styles.body}>
        {/* Dashboard variable filters */}
        {variables.length > 0 && <DashboardVariableBar />}

        {/* Widgets grid */}
        {widgets.length === 0 && variables.length === 0 ? (
          <EmptyState
            title="No widgets yet"
            text="Add widgets in the dashboard editor to display notebook cell outputs."
          />
        ) : widgets.length === 0 ? null : (
          <div ref={gridRef}>
            <GridLayout
              layout={widgets.map(toGridItem)}
              width={containerWidth}
              gridConfig={{ cols: settings?.grid_cols ?? 12, rowHeight: 30, margin: [4, 4] }}
              dragConfig={{ enabled: false }}
              resizeConfig={{ enabled: false }}
              style={{ minHeight: 240 }}
            >
              {widgets.map((widget) => (
                <div key={widget.id} style={styles.widgetCard}>
                  {isQueryWidget(widget) ? (
                    <QueryDataWidget
                      dashboardId={dashboard.id}
                      widget={widget}
                      canViewWithData={dashboard.can_view_with_data !== false}
                      queryEnabled={selected != null || !resolving}
                      viewerConnectorId={viewerConnectorId}
                      onFetchingChange={(fetching) => reportFetching(widget.id, fetching)}
                      registerRefresher={(fn) => registerWidgetRefresh(widget.id, fn)}
                    />
                  ) : (
                    <WidgetCard
                      widget={widget}
                      qc={qc}
                      widgetsData={dashboard.widgets_data}
                      dashboardId={id}
                      onFetchingChange={(fetching) => reportFetching(widget.id, fetching)}
                    />
                  )}
                </div>
              ))}
            </GridLayout>
          </div>
        )}
      </div>
      {showShare && dashboard && (
        <ShareModal
          resourceType="dashboard"
          resourceId={dashboard.id}
          canShare={dashboard.can_share ?? false}
          onClose={() => setShowShare(false)}
        />
      )}
    </AppShell>
    </DashboardVariablesProvider>
  )
}

export function DashboardPage() {
  const { id } = useParams<{ id: string }>()
  return <DashboardContent id={id ?? ''} />
}

const styles: Record<string, React.CSSProperties> = {
  loadingPage: {
    minHeight: '100vh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  loadingDot: {
    width: 8,
    height: 8,
    borderRadius: '50%',
    background: 'var(--accent)',
    opacity: 0.5,
  },
  subHeader: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    minHeight: 44,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    flexShrink: 0,
    position: 'sticky',
    top: 0,
    zIndex: 99,
  },
  headerLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    minWidth: 0,
  },
  backLink: {
    display: 'flex',
    alignItems: 'center',
    gap: 5,
    color: 'var(--nav-text)',
    textDecoration: 'none',
    fontSize: 13,
    fontWeight: 500,
    flexShrink: 0,
    opacity: 0.8,
  },
  breadcrumbSep: {
    color: 'var(--nav-text)',
    fontSize: 14,
    flexShrink: 0,
    opacity: 0.5,
  },
  dashboardTitle: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--nav-text)',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    maxWidth: 400,
  },
  liveIndicator: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 10,
    fontWeight: 600,
    letterSpacing: '0.04em',
    textTransform: 'uppercase',
    color: 'var(--nav-text-muted)',
    whiteSpace: 'nowrap',
  },
  liveDot: {
    display: 'inline-block',
    width: 6,
    height: 6,
    borderRadius: '50%',
    background: 'var(--success, #10b981)',
  },
  body: {
    flex: 1,
    margin: 0,
    padding: '8px 12px',
    width: '100%',
    display: 'flex',
    flexDirection: 'column',
    gap: 24,
  },
  widgetCard: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border-light)',
    borderRadius: 4,
    overflow: 'hidden',
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
  },
}
