import { useEffect, useRef, useState, useCallback, useLayoutEffect, useMemo } from 'react'
import { useParams, Link } from 'react-router-dom'
import { Loader2, Pencil, Settings, Globe, RefreshCw } from 'lucide-react'
import { ShareModal } from '../components/ShareModal'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard, Notebook, Cell, Widget } from '../types'
import type { ChartConfig } from '../charts/types'
import { mergeWidgetChartConfig, hasWidgetOverride, withWidgetOverride } from '../charts/widgetChartConfig'
import { formatExecutedAt } from '../utils/formatDateTime'
import { AppShell } from '../components/AppShell'
import { AppHeader, APP_CONTENT_MAX_WIDTH } from '../components/AppHeader'
import { EmptyState } from '../components/EmptyState'
import { OutputRenderer } from '../components/OutputRenderer'
import { DashboardVariablesProvider } from '../contexts/DashboardVariablesContext'
import { DashboardVariableBar } from '../components/DashboardVariableBar'
import { appearanceFrom } from '../utils/widgetAppearance'
import { textConfigFrom } from '../utils/textConfig'
import { isTextPanel } from '../utils/widgetKind'
import { QueryDataWidget } from '../components/QueryDataWidget'
import { WidgetFrame } from '../components/WidgetFrame'
import { TextWidget } from '../components/TextWidget'
import { ConnectorSelector } from '../components/ConnectorSelector'
import { useDashboardConnector } from '../hooks/useDashboardConnector'
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

/** TanStack's refetch resolves even on an errored query; detect that shape. */
function refreshErrored(result: unknown): boolean {
  return !!result && typeof result === 'object' && 'isError' in result && !!(result as { isError?: boolean }).isError
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

  // The card chrome is owned by the enclosing WidgetFrame; this component
  // only lays out the cell content inside it.
  return (
    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      <CellDataWidget widget={widget} qc={qc} widgetsData={widgetsData} dashboardId={dashboardId} loading={loading} onRun={handleRun} onEdit={onEdit} />
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

  // Per-widget fetch state, reported by the widgets themselves, so the header
  // can show one honest refreshing signal instead of a button that goes
  // idle before the widgets actually finish.
  const [fetchingWidgets, setFetchingWidgets] = useState<Record<string, boolean>>({})
  // One freshness signal for the whole app surface: the masthead's
  // "Updated …" line, moved by widget fetches and batch runs.
  const [lastFetchedAt, setLastFetchedAt] = useState<Date | null>(null)
  const refreshersRef = useRef(new Map<string, () => Promise<unknown>>())

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
    if (dashboard) {
      document.title = `${dashboard.title} — Aether Notebooks`
      const el = gridContainerRef.current
      if (el) setContainerWidth(el.clientWidth)
    }
    return () => { document.title = "Aether Notebooks" }
  }, [dashboard])

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
      // Each run settles as fulfilled only on success (a failed cell execute
      // or an errored query refresh rejects), so the fulfilled entries are
      // the runs that actually produced data.
      const cellRuns = cellWidgets.map(async w => {
        const resp = await fetch(`/api/v1/notebooks/${w.notebook_id}/cells/${w.cell_id}/execute`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            ...(token ? { Authorization: `Bearer ${token}` } : {}),
          },
          body: JSON.stringify({ parameters: {} }),
        })
        if (!resp.ok) throw new Error(`Cell run failed (${resp.status})`)
        return resp
      })
      const queryRuns = queryWidgets
        .map(w => refreshersRef.current.get(w.id))
        .filter((run): run is () => Promise<unknown> => !!run)
        .map(async run => {
          const result = await run()
          // The refresher resolves with TanStack's result even on an errored
          // refetch; surface that as a rejection for the tally below.
          if (refreshErrored(result)) throw new Error('Widget refresh failed')
          return result
        })
      const results = await Promise.allSettled([...cellRuns, ...queryRuns])
      // Never claim freshness for data that did not arrive.
      if (results.some(r => r.status === 'fulfilled')) {
        setLastFetchedAt(new Date())
      }
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

  const widgets = (dashboard?.widgets ?? []) as AnyWidget[]
  // Grid density and the refresh cadence are persisted dashboard settings, so
  // they are edit actions — viewers don't get to restyle the shared board.
  const canEdit = dashboard?.can_edit === true
  const autoRefreshSecs = dashboard?.settings?.auto_refresh_seconds ?? 0
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

  const variables = dashboard.settings?.variables ?? []
  const app = dashboard.settings?.app
  const appWidth = app?.width ?? 'fixed'
  // show_header=false hides the title block; the bar's escape hatches (back
  // link, actions) stay so the surface is never a dead end.
  const showMasthead = app?.show_header !== false
  const appTitle = app?.title || dashboard.title
  // Masthead, filter strip, and body share one content width (The Story).
  const contentWidth: React.CSSProperties | undefined = appWidth === 'fixed'
    ? { maxWidth: APP_CONTENT_MAX_WIDTH, margin: '0 auto' }
    : undefined

  return (
    <DashboardVariablesProvider dashboardId={dashboard.id} variables={variables} viewerConnectorId={viewerConnectorId}>
    <AppShell noPadding bare>
      {!showMasthead && <h1 className="sr-only">{appTitle}</h1>}
      <AppHeader
        title={showMasthead ? appTitle : ''}
        subtitle={showMasthead ? app?.subtitle : undefined}
        width={appWidth}
        showRefresh={app?.show_refresh !== false}
        showLastUpdated={app?.show_last_updated !== false}
        lastUpdated={lastFetchedAt}
        refreshing={refreshingCount > 0}
        onRefresh={() => executeAllWidgets(widgets)}
        backTo="/dashboards"
      >
          {/* Per-viewer connector selection: query widgets in the same
              warehouse run on the selected service; others keep their own.
              allowClear is off while a live default exists — clearing would
              just re-select the suggestion on the next render. */}
          <span
            title="Choose which connector this dashboard's queries run on for you"
            style={{ display: 'inline-flex', minWidth: 0 }}
          >
            <ConnectorSelector
              value={viewerConnectorId}
              onChange={select}
              types={['clickhouse']}
              allowClear={!suggestion}
              placeholder="Run widgets on…"
              // minWidth 0 lets the select shrink below its intrinsic width
              // when the wrapped action row is tight (tablet widths).
              style={{ fontSize: 12, maxWidth: 220, minHeight: 36, minWidth: 0 }}
            />
          </span>
          <Link to={`/dashboards/${id}`} className="app-action" title="Edit dashboard layout">
            <Settings size={12} /> Edit
          </Link>

          {dashboard.can_share !== false && (
            <button
              type="button"
              className="app-action"
              onClick={() => setShowShare(true)}
              title="Share dashboard"
            >
              <Globe size={12} /> Share
            </button>
          )}

          {/* Auto-refresh persists dashboard settings, so it is an editing
              control and rides the masthead only for editors. */}
          {canEdit && <div style={{ position: 'relative', display: 'inline-flex' }}>
            <select
              style={styles.actionSelect}
              value={refreshCustom ? 'custom' : String(refreshSeconds)}
              onChange={async e => {
                const val = e.target.value
                if (val === 'custom') { setRefreshCustom(true); return }
                setRefreshCustom(false)
                const secs = parseInt(val)
                setRefreshSeconds(secs)
                if (secs === autoRefreshSecs) return
                await api.put(`/api/v1/dashboards/${id}`, {
                  settings: { ...dashboard?.settings, auto_refresh_seconds: secs },
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
            <svg viewBox="0 0 10 6" width="10" height="6" style={{ position: 'absolute', right: 8, top: '50%', transform: 'translateY(-50%)', pointerEvents: 'none', fill: 'none', stroke: 'var(--text-muted)', strokeWidth: 1.5 }}>
              <polyline points="1,1 5,5 9,1" />
            </svg>
          </div>}
          {canEdit && refreshCustom && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
              <input
                type="text"
                style={styles.actionInput}
                value={refreshSeconds}
                onChange={e => {
                  const val = parseInt(e.target.value)
                  if (!isNaN(val) && val >= 0) setRefreshSeconds(val)
                }}
                onBlur={async () => {
                  if (refreshSeconds === autoRefreshSecs) return
                  await api.put(`/api/v1/dashboards/${id}`, {
                    settings: { ...dashboard?.settings, auto_refresh_seconds: refreshSeconds },
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
      </AppHeader>

      {/* Quiet filter strip: the variable bar loses its panel chrome inside
          (see .app-filter-strip) and is constrained to the masthead width. */}
      {variables.length > 0 && (
        <div className="app-filter-strip">
          <div style={contentWidth}>
            <DashboardVariableBar />
          </div>
        </div>
      )}

      <div className="app-body">
        <div style={{ ...contentWidth, display: 'flex', flexDirection: 'column', gap: 24 }}>
        {/* Widgets grid */}
        {widgets.length === 0 && variables.length === 0 ? (
          <EmptyState
            title="This app has no widgets yet"
            text="Add widgets in the dashboard editor."
          />
        ) : widgets.length === 0 ? null : (
          <div ref={gridRef}>
            <GridLayout
              layout={widgets.map(toGridItem)}
              width={containerWidth}
              gridConfig={{ cols: dashboard.settings?.grid_cols ?? 12, rowHeight: 30, margin: [4, 4] }}
              dragConfig={{ enabled: false }}
              resizeConfig={{ enabled: false }}
              style={{ minHeight: 240 }}
            >
              {widgets.map((widget) => (
                <WidgetFrame key={widget.id} appearance={appearanceFrom(widget.config)}>
                  {isQueryWidget(widget) ? (
                    <QueryDataWidget
                      dashboardId={dashboard.id}
                      widget={widget}
                      canViewWithData={dashboard.can_view_with_data !== false}
                      queryEnabled={selected != null || !resolving}
                      viewerConnectorId={viewerConnectorId}
                      onFetchingChange={(fetching) => reportFetching(widget.id, fetching)}
                      onFetched={() => setLastFetchedAt(new Date())}
                      registerRefresher={(fn) => registerWidgetRefresh(widget.id, fn)}
                    />
                  ) : isTextPanel(widget) ? (
                    <TextWidget markdown={textConfigFrom(widget.config).markdown} />
                  ) : (
                    <WidgetCard
                      widget={widget}
                      qc={qc}
                      widgetsData={dashboard.widgets_data}
                      dashboardId={id}
                      onFetchingChange={(fetching) => reportFetching(widget.id, fetching)}
                    />
                  )}
                </WidgetFrame>
              ))}
            </GridLayout>
          </div>
        )}
        </div>
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
  // Masthead controls sit on the page canvas (not the dark nav chrome), so
  // they use the body token family. min-height 36 keeps the app surface's
  // ≥36px tap-target rule uniform across the action row.
  actionSelect: {
    fontSize: 12,
    minHeight: 36,
    padding: '4px 24px 4px 8px',
    cursor: 'pointer',
    appearance: 'none',
    WebkitAppearance: 'none',
    MozAppearance: 'none',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
  },
  actionInput: {
    width: 72,
    fontSize: 12,
    minHeight: 36,
    padding: '4px 6px',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
  },
}
