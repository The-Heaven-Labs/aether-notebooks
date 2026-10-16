import { useEffect, useRef, useState, useCallback, useLayoutEffect, useMemo } from 'react'
import { useParams, Link, useSearchParams } from 'react-router-dom'
import { Loader2, Pencil, Settings, Globe, RefreshCw, Eye } from 'lucide-react'
import { ShareModal } from '../components/ShareModal'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, getToken } from '../api/client'
import { getApiUrl } from '../config'
import type { Dashboard, Notebook, Cell, Widget } from '../types'
import type { ChartConfig } from '../charts/types'
import { mergeWidgetChartConfig, hasWidgetOverride, withWidgetOverride } from '../charts/widgetChartConfig'
import { formatExecutedAt } from '../utils/formatDateTime'
import { AppShell } from '../components/AppShell'
import { AppHeader, APP_CONTENT_MAX_WIDTH } from '../components/AppHeader'
import { AppThemeProvider } from '../components/AppThemeProvider'
import { EmptyState } from '../components/EmptyState'
import { OutputRenderer } from '../components/OutputRenderer'
import { CollaboratorAvatars } from '../components/CollaboratorAvatars'
import { DashboardVariablesProvider } from '../contexts/DashboardVariablesContext'
import { DashboardVariableBar } from '../components/DashboardVariableBar'
import { AppTabs, appTabButtonId, appTabPanelId, EmptyChapter } from '../components/AppTabs'
import { activeTabId, widgetTabId, NO_TABS, tabQueryString } from '../utils/dashboardTabs'
import { variablesForChapter } from '../utils/chapterFilters'
import { appearanceFrom } from '../utils/widgetAppearance'
import { textConfigFrom } from '../utils/textConfig'
import { htmlConfigFrom } from '../utils/htmlConfig'
import { isCustomWidget, isHTMLPanel, isTextPanel } from '../utils/widgetKind'
import { customConfigFrom } from '../utils/customConfig'
import { toGridItem } from '../utils/widgetLayout'
import {
  createPerWidgetDebouncer,
  diffVariableDefinitions,
  diffWidgetRuns,
  widgetsReferencingTokens,
} from '../utils/dashboardRunSignature'
import type { PerWidgetDebouncer, VariableDefinitionSnapshot, WidgetRunBaseline } from '../utils/dashboardRunSignature'
import { QueryDataWidget } from '../components/QueryDataWidget'
import { WidgetFrame } from '../components/WidgetFrame'
import { TextWidget } from '../components/TextWidget'
import { HtmlWidget } from '../components/HtmlWidget'
import { CustomWidget } from '../components/CustomWidget'
import { ConnectorSelector } from '../components/ConnectorSelector'
import { useDashboardConnector } from '../hooks/useDashboardConnector'
import { useDashboardDoc } from '../hooks/useDashboardDoc'
import { useAuth } from '../hooks/useAuth'
import { GridLayout } from 'react-grid-layout'
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

function DashboardContent({ id }: { id: string }) {
  const qc = useQueryClient()
  const { user } = useAuth()
  // The active chapter rides the URL (`?tab=`) so shared links and reloads
  // land on the same tab.
  const [searchParams, setSearchParams] = useSearchParams()
  // Editors opt into the draft with `?preview=draft`; the default view is the
  // published app.
  const previewRequested = searchParams.get('preview') === 'draft'
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

  // The published snapshot and the draft are distinct server reads, so they
  // get distinct cache entries: switching modes must never serve the other
  // mode's rows while the fetch is in flight.
  const { data: dashboard, isLoading, error } = useQuery({
    queryKey: ['dashboard', id, previewRequested ? 'draft' : 'published'],
    queryFn: () => api.get<DashboardWithWidgets>(
      // `?published=1` asks for the latest snapshot; the plain fetch serves
      // editors the draft (viewers always get the snapshot server-side).
      previewRequested ? `/api/v1/dashboards/${id}` : `/api/v1/dashboards/${id}?published=1`,
    ),
    enabled: !!id,
  })

  // A response that served the published snapshot identifies it; the surface
  // then renders entirely from REST and never subscribes to the draft doc.
  const published = dashboard?.published_version != null
  const canEdit = dashboard?.can_edit === true
  // Editors asking for `?preview=draft` get the draft; a viewer without edit
  // is served the snapshot anyway, so the flag is a no-op for them.
  const draftPreview = canEdit && previewRequested && !published

  // Live dashboard document: the source of truth for title/settings/widgets
  // once synced. Only the response identifies a published snapshot, so the
  // document is not acquired until it arrives — a pre-response connect would
  // subscribe to the draft doc (and a viewer is denied by the relay once a
  // version exists, noisily). Published views never connect; draft previews
  // keep the #245 behavior, read-only for non-editors. When the relay is
  // unreachable the document never syncs and the REST snapshot keeps painting
  // the page.
  const liveDoc = useDashboardDoc(id, {
    enabled: canEdit && !published,
    connect: !!dashboard && !published,
  })
  // The document only wins when this surface is not serving a published
  // snapshot — a published view must never merge draft state — and it
  // actually carries content: a synced but zero-struct document is a stale
  // relay serving an empty update (the backend seed always writes the meta
  // entries), and handing over to it would blank the widget list while REST
  // stayed correct.
  const liveSynced = liveDoc.synced && liveDoc.hasDocumentContent && !published
  const liveTitle = liveSynced && liveDoc.title ? liveDoc.title : (dashboard?.title ?? '')

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

  // Cache-eligible re-run handles, registered by query widgets. Auto re-runs
  // on live definition changes must flow through the shared query cache —
  // only the manual Refresh action (refreshersRef) bypasses it.
  const [rerunners] = useState(() => new Map<string, () => Promise<unknown>>())
  const registerWidgetRerun = useCallback((widgetId: string, fn: () => Promise<unknown>) => {
    rerunners.set(widgetId, fn)
    return () => { rerunners.delete(widgetId) }
  }, [rerunners])

  // Per-widget debouncer for auto re-runs: a collaborator typing SQL fires a
  // document update per keystroke, so runs are coalesced (~2s) per widget.
  // Stable for the page's lifetime (useState initializer, not useRef writes).
  const [runDebouncer] = useState<PerWidgetDebouncer>(() => createPerWidgetDebouncer((widgetId) => {
    void rerunners.get(widgetId)?.()
  }))

  // Baselines for live-document change detection. `null` until the first
  // sync seeds them, so the REST → doc handover itself never re-runs widgets.
  const widgetRunBaselinesRef = useRef<Record<string, WidgetRunBaseline> | null>(null)
  const variableDefinitionsRef = useRef<Record<string, VariableDefinitionSnapshot> | null>(null)
  useEffect(() => () => runDebouncer.cancelAll(), [runDebouncer])

  const refreshingCount = useMemo(
    () => Object.values(fetchingWidgets).filter(Boolean).length,
    [fetchingWidgets],
  )

  useEffect(() => {
    if (dashboard) {
      document.title = liveTitle ? `${liveTitle} — Aether Notebooks` : 'Aether Notebooks'
      const el = gridContainerRef.current
      if (el) setContainerWidth(el.clientWidth)
    }
    return () => { document.title = "Aether Notebooks" }
  }, [dashboard, liveTitle])

  // Ref guard (not state): the stabilized auto-refresh interval captures this
  // function once, so it must not close over a stale `isRunningAll`.
  const runningAllRef = useRef(false)
  async function executeAllWidgets(widgetList: AnyWidget[]) {
    if (runningAllRef.current) return
    const token = localStorage.getItem('aether_token')
    const cellWidgets = widgetList.filter(w => !isQueryWidget(w) && w.notebook_id && w.cell_id)
    const queryWidgets = widgetList.filter(isQueryWidget)
    if (!cellWidgets.length && !queryWidgets.length) return
    runningAllRef.current = true
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
      runningAllRef.current = false
      setIsRunningAll(false)
    }
  }

  // Once the document has synced it is the source of truth for the widget
  // list; until then the REST payload paints the page. Timestamps are not
  // document fields, so doc widgets inherit `created_at` from their REST rows.
  const widgets = useMemo<AnyWidget[]>(() => {
    const rest = dashboard?.widgets ?? []
    if (!liveSynced) return rest as AnyWidget[]
    const restById = new Map(rest.map(w => [w.id, w]))
    return liveDoc.widgets.map(w => {
      const restWidget = restById.get(w.id)
      return (restWidget?.created_at ? { ...w, created_at: restWidget.created_at } : w) as AnyWidget
    })
  }, [liveSynced, liveDoc.widgets, dashboard?.widgets])

  // Live-merged settings: the document is authoritative for title, settings,
  // and variables once synced. `can_*` flags and cell outputs (`widgets_data`)
  // are not document fields and stay on the REST row.
  const settings = useMemo(
    () => (liveSynced
      ? { ...(dashboard?.settings ?? {}), ...liveDoc.settings, variables: liveDoc.variables }
      : dashboard?.settings),
    [liveSynced, liveDoc.settings, liveDoc.variables, dashboard?.settings],
  )

  // Chapters: `settings.tabs` arrives with the dashboard response (draft or
  // published snapshot). No tabs means a single implicit chapter holding every
  // widget, so the render path below stays uniform.
  const tabs = settings?.tabs ?? NO_TABS
  const requestedTab = searchParams.get('tab')
  const activeTab = activeTabId(tabs, requestedTab)
  // Clear invalid or inapplicable `?tab=` values once the dashboard is loaded
  // so an unknown id or a no-tab dashboard never leaves a stale parameter.
  // Other query params (preview, variable pins) are preserved.
  const tabsKnown = !!dashboard
  useEffect(() => {
    if (requestedTab === null || !tabsKnown) return
    const isValid = tabs.length > 0 && tabs.some((t) => t.id === requestedTab)
    if (!isValid) {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          next.delete('tab')
          return next
        },
        { replace: true },
      )
    }
  }, [requestedTab, tabs, tabsKnown, setSearchParams])
  // One grid per chapter: every widget resolves to exactly one chapter (an
  // empty or unknown tab falls back to the first), and a chapter's items feed
  // only its own grid — no shared coordinate space, and each chapter compacts
  // independently. All chapters stay mounted so refreshers stay registered.
  const chapters = useMemo(() => {
    if (tabs.length === 0) return [{ tabId: null as string | null, widgets }]
    return tabs.map((tab) => ({
      tabId: tab.id as string | null,
      widgets: widgets.filter((w) => widgetTabId(w, tabs) === tab.id),
    }))
  }, [tabs, widgets])
  // Replacing (not pushing) keeps tab switches out of the back stack, like
  // the filter strip's own URL writes.
  const handleTabChange = useCallback((tabId: string) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev)
      next.set('tab', tabId)
      return next
    }, { replace: true })
  }, [setSearchParams])

  // Definitions and the app block are read before the early returns so the
  // filter scope (which picks the visible variable set) is a stable hook.
  const variables = settings?.variables ?? []
  const app = settings?.app ?? dashboard?.settings?.app
  // Full custom page (`settings.app.layout: "custom"`): the first custom
  // component (widgets arrive created_at-ordered) renders full-bleed with no
  // card chrome; every other widget is a hidden data source for its bindings.
  // No custom widget → today's grid.
  const customPage = app?.layout === 'custom' ? widgets.find(isCustomWidget) : undefined
  // Filter scoping: under `filter_scope: "tab"` the strip shows only the
  // active chapter's variables (plus everything they depend on); the default
  // `all` keeps every filter visible, and tabs-free dashboards never scope.
  const scopedVariables = useMemo(
    () => (app?.filter_scope === 'tab'
      ? variablesForChapter(widgets, variables, tabs, activeTab ?? '')
      : variables),
    [app?.filter_scope, widgets, variables, tabs, activeTab],
  )

  // Grid density and the refresh cadence are persisted dashboard settings, so
  // they are edit actions — viewers don't get to restyle the shared board.
  const autoRefreshSecs = settings?.auto_refresh_seconds ?? 0
  const PRESET_REFRESH = [0, 30, 60, 300, 600]
  const customRefreshText = refreshCustom && refreshSeconds > 0 ? ` — ${refreshSeconds}s` : ''

  // Sync local state from dashboard data
  useEffect(() => {
    setRefreshSeconds(autoRefreshSecs)
    setRefreshCustom(!PRESET_REFRESH.includes(autoRefreshSecs))
  }, [autoRefreshSecs])

  // Stable inputs for the auto-refresh interval: doc updates produce a fresh
  // `widgets` array on every collaborator keystroke, and depending on that
  // identity used to restart (and postpone) the timer continuously. The
  // interval now restarts only when the cadence or the widget id set changes,
  // and reads the latest widgets from the ref when it fires.
  const widgetsRef = useRef(widgets)
  const executeAllRef = useRef(executeAllWidgets)
  useEffect(() => {
    widgetsRef.current = widgets
    executeAllRef.current = executeAllWidgets
  })
  const widgetIdsKey = useMemo(() => widgets.map(w => w.id).join('|'), [widgets])

  useEffect(() => {
    if (!refreshSeconds || refreshSeconds <= 0 || !widgetIdsKey) return
    const intervalId = setInterval(() => { void executeAllRef.current(widgetsRef.current) }, refreshSeconds * 1000)
    return () => clearInterval(intervalId)
  }, [refreshSeconds, widgetIdsKey])

  // Live definition changes → re-run only the widgets that actually changed.
  // The document projection gives every widget a new identity on any update,
  // so this diffs per-widget run signatures against baselines seeded on the
  // first sync (the REST → doc takeover itself must not re-run anything).
  useEffect(() => {
    if (!liveSynced) {
      // REST still paints the page; drop baselines so the handover seeds
      // fresh ones instead of diffing against stale state.
      widgetRunBaselinesRef.current = null
      variableDefinitionsRef.current = null
      runDebouncer.cancelAll()
      return
    }

    const runDiff = diffWidgetRuns(widgetRunBaselinesRef.current, widgets)
    widgetRunBaselinesRef.current = runDiff.next
    for (const widgetId of runDiff.removed) runDebouncer.cancel(widgetId)
    for (const widgetId of runDiff.rerun) runDebouncer.schedule(widgetId)
    for (const change of runDiff.cellRefChanged) {
      // Cell widgets render stored outputs: refresh the referenced data
      // (embedded `widgets_data`, or the notebook when outputs are not
      // inlined) instead of executing anything.
      if (dashboard?.can_view_with_data) {
        qc.invalidateQueries({ queryKey: ['dashboard', id] })
      } else if (change.notebookId) {
        qc.invalidateQueries({ queryKey: ['notebook', change.notebookId] })
      }
    }

    // Variable definition/default edits only affect widgets that reference
    // the changed token. A live-added variable re-runs referencing widgets so
    // the server applies the declaration default for this viewer.
    const variableDiff = diffVariableDefinitions(variableDefinitionsRef.current, liveDoc.variables)
    variableDefinitionsRef.current = variableDiff.next
    for (const widgetId of widgetsReferencingTokens(widgets, variableDiff.tokens)) {
      runDebouncer.schedule(widgetId)
    }
  }, [liveSynced, liveDoc.widgets, liveDoc.variables, widgets, dashboard?.can_view_with_data, id, qc, runDebouncer])

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

  const appWidth = app?.width ?? 'fixed'
  // show_header=false hides the title block; the bar's escape hatches (back
  // link, actions) stay so the surface is never a dead end.
  const showMasthead = app?.show_header !== false
  // The live document's title wins once bound; fall back to the row.
  const appTitle = app?.title || liveTitle
  // Masthead, filter strip, and body share one content width (The Story).
  const contentWidth: React.CSSProperties | undefined = appWidth === 'fixed'
    ? { maxWidth: APP_CONTENT_MAX_WIDTH, margin: '0 auto' }
    : undefined

  // The dashboard logo wins over the org fallback. The authed attachment route
  // needs credentials an <img> cannot send as a header, so the session JWT
  // rides the query string (the auth middleware accepts ?token= for these).
  // The runtime API base is prefixed so split-origin deployments resolve it.
  const logoAttachmentId = app?.logo_attachment_id || dashboard.org_branding?.logo_attachment_id
  const sessionToken = getToken()
  const logoUrl = logoAttachmentId
    ? `${getApiUrl()}/api/v1/attachments/${logoAttachmentId}${sessionToken ? `?token=${encodeURIComponent(sessionToken)}` : ''}`
    : undefined

  // Mode-toggle links preserve the rest of the query string (`?tab=`,
  // variable pins) so switching modes never resets the reader's place.
  const previewSearch = (() => {
    const params = new URLSearchParams(searchParams)
    params.set('preview', 'draft')
    return `?${params.toString()}`
  })()
  const publishedSearch = (() => {
    const params = new URLSearchParams(searchParams)
    params.delete('preview')
    const rest = params.toString()
    return rest ? `?${rest}` : ''
  })()

  return (
    <AppThemeProvider theme={app?.theme} branding={dashboard.org_branding}>
    <DashboardVariablesProvider dashboardId={dashboard.id} variables={scopedVariables} allVariables={variables} viewerConnectorId={viewerConnectorId} published={published}>
    <AppShell noPadding bare>
      {!showMasthead && <h1 className="sr-only">{appTitle}</h1>}
      <AppHeader
        title={showMasthead ? appTitle : ''}
        subtitle={showMasthead ? app?.subtitle : undefined}
        width={appWidth}
        showRefresh={app?.show_refresh !== false}
        showLastUpdated={app?.show_last_updated !== false}
        lastUpdated={lastFetchedAt}
        refreshing={refreshingCount > 0 || isRunningAll}
        onRefresh={() => executeAllWidgets(widgets)}
        backTo="/dashboards"
        logoUrl={logoUrl}
      >
        {/* Presence + live state: avatars come from the document's awareness
            and the indicator lights while the relay is connected. A published
            view never connects, so nothing renders here. When the relay is
            unavailable nothing renders here and the page keeps serving the
            REST snapshot. */}
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
          <CollaboratorAvatars
            awareness={published ? null : liveDoc.awareness}
            currentUserEmail={userEmail}
          />
          {!published && liveDoc.connected && liveDoc.hasDocumentContent && (
            <span style={styles.liveIndicator} title="Live updates from collaborators">
              <span style={styles.liveDot} /> Live
            </span>
          )}
        </span>
          {/* Published/draft mode: the badge marks a draft preview and the
              link is the way back to (or into) the published app. Editors
              only — viewers never get the draft. */}
          {draftPreview && <span style={styles.draftChip}>Draft preview</span>}
          {canEdit && (published || draftPreview) && (
            draftPreview ? (
              <Link
                to={{ search: publishedSearch }}
                className="app-action"
                title="Back to the published app"
              >
                <Eye size={12} /> View published
              </Link>
            ) : (
              <Link
                to={{ search: previewSearch }}
                className="app-action"
                title="Preview unpublished draft changes"
              >
                <Eye size={12} /> Preview draft
              </Link>
            )
          )}
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
          <Link to={`/dashboards/${id}${tabQueryString(activeTab)}`} className="app-action" title="Edit dashboard layout">
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
              control that only belongs on the draft surface: in published mode
              `settings` is the snapshot, and a wholesale write would merge
              snapshot tabs/theme/variables back into the draft. The interval
              itself still runs from the snapshot's cadence. */}
          {canEdit && !published && <div style={{ position: 'relative', display: 'inline-flex' }}>
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
            <svg viewBox="0 0 10 6" width="10" height="6" style={{ position: 'absolute', right: 8, top: '50%', transform: 'translateY(-50%)', pointerEvents: 'none', fill: 'none', stroke: 'var(--text-muted)', strokeWidth: 1.5 }}>
              <polyline points="1,1 5,5 9,1" />
            </svg>
          </div>}
          {canEdit && !published && refreshCustom && (
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
      </AppHeader>

      {/* Chapter tablist (The Story): the dashboard's tabs under the masthead;
          hidden entirely when no tabs are defined, and on the full custom page
          (chapters do not apply to a single full-bleed component). */}
      {tabs.length > 0 && !customPage && (
        <div className="app-tabs">
          <div style={contentWidth}>
            <AppTabs tabs={tabs} activeId={activeTab} onChange={handleTabChange} />
          </div>
        </div>
      )}

      {/* Quiet filter strip: the variable bar loses its panel chrome inside
          (see .app-filter-strip) and is constrained to the masthead width.
          Under `filter_scope: "tab"` only the active chapter's filters list. */}
      {scopedVariables.length > 0 && (
        <div className="app-filter-strip">
          <div style={contentWidth}>
            <DashboardVariableBar />
          </div>
        </div>
      )}

      <div className="app-body">
        <div style={{ ...contentWidth, display: 'flex', flexDirection: 'column', gap: 24 }}>
        {/* Widgets grid (or the full custom page: one component, no chrome) */}
        {customPage ? (
          <div style={{ width: '100%', minHeight: 480, height: 'calc(100vh - 220px)' }}>
            <CustomWidget
              {...customConfigFrom(customPage.config)}
              enabled={dashboard.custom_code_enabled === true}
              widgets={widgets}
              endpointBase={`/api/v1/dashboards/${id}`}
              published={published}
              viewerConnectorId={viewerConnectorId}
              widgetsData={dashboard.widgets_data}
              dataEnabled={dashboard.can_view_with_data !== false}
              theme={app?.theme}
              branding={dashboard.org_branding}
            />
          </div>
        ) : widgets.length === 0 && variables.length === 0 ? (
          <EmptyState
            title="This app has no widgets yet"
            text="Add widgets in the editor — viewers see them here as chapters, filters, and readings."
          />
        ) : widgets.length === 0 ? null : (
          <div ref={gridRef}>
            {chapters.map(({ tabId, widgets: chapterWidgets }) => (
              <div
                key={tabId ?? '__all__'}
                id={tabId ? appTabPanelId(tabId) : undefined}
                role={tabId ? 'tabpanel' : undefined}
                aria-labelledby={tabId ? appTabButtonId(tabId) : undefined}
                // Inactive chapters stay mounted (their refreshers stay
                // registered and their data loads in one pass) but are hidden
                // from view.
                style={tabId !== null && tabId !== activeTab ? { display: 'none' } : undefined}
              >
                {chapterWidgets.length === 0 ? (
                  <EmptyChapter />
                ) : (
                  <GridLayout
                    layout={chapterWidgets.map(toGridItem)}
                    width={containerWidth}
                    gridConfig={{ cols: settings?.grid_cols ?? 12, rowHeight: 30, margin: [4, 4] }}
                    dragConfig={{ enabled: false }}
                    resizeConfig={{ enabled: false }}
                    style={{ minHeight: 240 }}
                  >
                    {chapterWidgets.map((widget) => (
                      <WidgetFrame key={widget.id} appearance={appearanceFrom(widget.config)}>
                        {isQueryWidget(widget) ? (
                          <QueryDataWidget
                            dashboardId={dashboard.id}
                            widget={widget}
                            canViewWithData={dashboard.can_view_with_data !== false}
                            queryEnabled={selected != null || !resolving}
                            viewerConnectorId={viewerConnectorId}
                            published={published}
                            onFetchingChange={(fetching) => reportFetching(widget.id, fetching)}
                            onFetched={() => setLastFetchedAt(new Date())}
                            registerRefresher={(fn) => registerWidgetRefresh(widget.id, fn)}
                            registerRerunner={(fn) => registerWidgetRerun(widget.id, fn)}
                          />
                        ) : isTextPanel(widget) ? (
                          <TextWidget markdown={textConfigFrom(widget.config).markdown} />
                        ) : isHTMLPanel(widget) ? (
                          <HtmlWidget
                            html={htmlConfigFrom(widget.config).html}
                            css={htmlConfigFrom(widget.config).css}
                            enabled={dashboard.html_panels_enabled !== false}
                          />
                        ) : isCustomWidget(widget) ? (
                          <CustomWidget
                            {...customConfigFrom(widget.config)}
                            enabled={dashboard.custom_code_enabled === true}
                            widgets={widgets}
                            endpointBase={`/api/v1/dashboards/${id}`}
                            published={published}
                            viewerConnectorId={viewerConnectorId}
                            widgetsData={dashboard.widgets_data}
                            dataEnabled={dashboard.can_view_with_data !== false}
                            theme={app?.theme}
                            branding={dashboard.org_branding}
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
                      </WidgetFrame>
                    ))}
                  </GridLayout>
                )}
              </div>
            ))}
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
    </AppThemeProvider>
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
  liveIndicator: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 10,
    fontWeight: 600,
    letterSpacing: '0.04em',
    textTransform: 'uppercase',
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap',
  },
  // Muted mode marker: the surface is showing the unpublished draft.
  draftChip: {
    display: 'inline-flex',
    alignItems: 'center',
    fontSize: 10,
    fontWeight: 600,
    letterSpacing: '0.04em',
    textTransform: 'uppercase',
    color: 'var(--text-muted)',
    background: 'var(--bg-input)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: '2px 8px',
    whiteSpace: 'nowrap',
  },
  liveDot: {
    display: 'inline-block',
    width: 6,
    height: 6,
    borderRadius: '50%',
    background: 'var(--success, #10b981)',
  },
}
