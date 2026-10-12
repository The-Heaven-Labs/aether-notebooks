import { useState, useEffect, useRef, useCallback, useLayoutEffect, useMemo } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft, X, Plus, Eye, Pencil, Shield, LayoutTemplate } from 'lucide-react'
import { AppShell } from '../components/AppShell'
import { DashboardVariablesProvider } from '../contexts/DashboardVariablesContext'
import { DashboardVariableBar } from '../components/DashboardVariableBar'
import { rescaleWidgetLayouts } from '../utils/dashboardGrid'
import { buildAddWidgetPayload, validTypesForSource } from '../utils/addWidgetPayload'
import type { AddWidgetSource, AddWidgetType } from '../utils/addWidgetPayload'
import { isQueryWidget, isTextPanel, widgetKindLabel } from '../utils/widgetKind'
import { toGridItem } from '../utils/widgetLayout'
import { widgetTabId, NO_TABS } from '../utils/dashboardTabs'
import { appearanceFrom } from '../utils/widgetAppearance'
import { textConfigFrom } from '../utils/textConfig'
import { EmptyState } from '../components/EmptyState'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../api/client'
import type { Dashboard, DashboardVariableType, Notebook, Cell, Widget } from '../types'
import type { ChartConfig } from '../charts/types'
import { mergeWidgetChartConfig, hasWidgetOverride, withWidgetOverride } from '../charts/widgetChartConfig'
import { OutputRenderer } from '../components/OutputRenderer'
import { QueryDataWidget } from '../components/QueryDataWidget'
import { WidgetFrame } from '../components/WidgetFrame'
import { TextWidget } from '../components/TextWidget'
import { ErrorBanner } from '../components/ErrorBanner'
import { GridLayout } from 'react-grid-layout'
import type { LayoutItem, Layout } from 'react-grid-layout'
import { Skeleton } from '../components/Skeleton'
import { CollaboratorAvatars } from '../components/CollaboratorAvatars'
import { PermissionsPanel } from '../components/PermissionsPanel'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { WidgetConfigDrawer } from '../components/WidgetConfigDrawer'
import { DashboardVariablesPanel } from '../components/DashboardVariablesPanel'
import { AppSettingsPanel } from '../components/AppSettingsPanel'
import { DashboardVersionsPanel, useDashboardVersions } from '../components/DashboardVersionsPanel'
import { ConnectorSelector } from '../components/ConnectorSelector'
import { SqlEditor } from '../components/SqlEditor'
import { useDashboardDoc } from '../hooks/useDashboardDoc'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

interface DashboardWithWidgets extends Dashboard {
  widgets: Widget[]
}


function nextWidgetLayout(widgets: Widget[]): { row: number; col: number; width: number; height: number } {
  if (!widgets.length) return { row: 0, col: 0, width: 6, height: 8 }
  const maxBottom = widgets.reduce((max, w) => Math.max(max, w.layout.row + w.layout.height), 0)
  return { row: maxBottom, col: 0, width: 6, height: 8 }
}

/** Human name for a widget's controls; falls back to a generic label. */
function widgetDisplayName(widget: Widget): string {
  const cfg = widget.config as Record<string, unknown> | undefined
  const t = typeof cfg?.title === 'string' ? cfg.title : typeof cfg?.label === 'string' ? cfg.label : ''
  return t || 'widget'
}

/**
 * A refused publish (422) carries the per-widget projection warnings that
 * blocked it; fold them into the single message the error banner shows.
 */
function publishErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const warnings = (err.body as { warnings?: unknown } | null)?.warnings
    if (Array.isArray(warnings) && warnings.length > 0) {
      return `${err.message}: ${warnings.join('; ')}`
    }
  }
  return err instanceof Error ? err.message : 'Publish failed'
}

/** Drop coordinates of a drag/resize stop event (mouse or touch). */
function eventPoint(event: Event): { x: number; y: number } | null {
  if ('clientX' in event && 'clientY' in event) {
    const { clientX, clientY } = event as MouseEvent
    if (Number.isFinite(clientX) && Number.isFinite(clientY)) return { x: clientX, y: clientY }
  }
  const touch = (event as TouchEvent).changedTouches?.[0]
  return touch ? { x: touch.clientX, y: touch.clientY } : null
}

/**
 * The `data-tab-id` chip under a drop point: a tab id, "" for the All chip
 * (clears the placement), or null when the drop landed anywhere else. The
 * full hit-test stack is scanned because the dragged card floats above the
 * strip at drop time and would otherwise shadow the chip.
 */
function tabIdAtPoint(event: Event): string | null {
  const point = eventPoint(event)
  if (!point) return null
  const stack: Array<Element | null> = typeof document.elementsFromPoint === 'function'
    ? document.elementsFromPoint(point.x, point.y)
    : [document.elementFromPoint(point.x, point.y)]
  for (const el of stack) {
    const chip = el?.closest('[data-tab-id]')
    if (chip) return chip.getAttribute('data-tab-id') ?? ''
  }
  return null
}

function WidgetContent({ widget, editingEnabled, onConfigSave, onConfigReset }: { widget: Widget; editingEnabled: boolean; onConfigSave: (widgetId: string, config: ChartConfig) => void; onConfigReset: (widgetId: string) => void }) {
  const { data: notebook, isLoading } = useQuery({
    queryKey: ['notebook', widget.notebook_id],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${widget.notebook_id}`),
  })

  if (isLoading) return <div style={widgetContentStyles.loading}>Loading…</div>

  const cell = notebook?.cells?.find((c: Cell) => c.id === widget.cell_id)
  if (!cell) return <div style={widgetContentStyles.empty}>Cell not found</div>

  // Markdown cells render their source directly — they don't need to be "run"
  if (cell.type === 'text') {
    return (
      <div style={widgetContentStyles.markdown}>
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{cell.source || ''}</ReactMarkdown>
      </div>
    )
  }

  if (!cell.outputs?.length) {
    return <div style={widgetContentStyles.empty}>No results yet — run the notebook first</div>
  }
  const fixedView = widget.type === 'chart' ? 'chart' : 'table'
  const chartConfig = mergeWidgetChartConfig(cell.metadata?.chart, widget.config)
  const chartOverridden = hasWidgetOverride(widget.config)
  return (
    <OutputRenderer
      outputs={cell.outputs}
      fixedView={fixedView}
      chartConfig={chartConfig}
      // Chart config edits are document writes; omit the callbacks entirely
      // while editing is paused so the config UI never opens.
      onChartConfigChange={editingEnabled ? (config) => onConfigSave(widget.id, config) : undefined}
      chartConfigOverridden={chartOverridden}
      onChartConfigReset={editingEnabled ? () => onConfigReset(widget.id) : undefined}
    />
  )
}

const widgetContentStyles: Record<string, React.CSSProperties> = {
  loading: { padding: '16px', fontSize: 13, color: 'var(--text-muted)' },
  empty: { padding: '16px', fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  markdown: { padding: '16px', fontSize: 14, color: 'var(--text-primary)', lineHeight: 1.6, overflow: 'auto', height: '100%' },
}

export function DashboardEditorPage() {
  const { id } = useParams<{ id: string }>()
  const qc = useQueryClient()

  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [mutationError, setMutationError] = useState<string | null>(null)
const [saveStatus, setSaveStatus] = useState<'saving' | 'saved' | null>(null)
const pendingSaves = useRef(0)
const saveStatusTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

const markSaving = useCallback(() => {
  pendingSaves.current++
  setSaveStatus('saving')
  if (saveStatusTimer.current) clearTimeout(saveStatusTimer.current)
}, [])

const markSaved = useCallback(() => {
  pendingSaves.current--
  if (pendingSaves.current <= 0) {
    pendingSaves.current = 0
    setSaveStatus('saved')
    saveStatusTimer.current = setTimeout(() => setSaveStatus(null), 2000)
  }
}, [])

  const [showPicker, setShowPicker] = useState(false)
  const [showPermissions, setShowPermissions] = useState(false)
  const [pickerSource, setPickerSource] = useState<'cell' | 'query' | 'none'>('cell')
  const [pickerNotebookId, setPickerNotebookId] = useState('')
  const [pickerCellId, setPickerCellId] = useState('')
  const [pickerConnectorId, setPickerConnectorId] = useState<string | null>(null)
  const [pickerQuery, setPickerQuery] = useState('SELECT 1')
  const [pickerType, setPickerType] = useState<AddWidgetType>('table')
  const [pickerMarkdown, setPickerMarkdown] = useState('')
  const [pickerError, setPickerError] = useState<string | null>(null)
  const [editingWidget, setEditingWidget] = useState<Widget | null>(null)
  const [showVariables, setShowVariables] = useState(false)
  const [showAppSettings, setShowAppSettings] = useState(false)
  // Editor-local chapter filter (NOT URL state) and the App settings panel's
  // opening section (the strip's + reveals Tabs).
  const [editorTab, setEditorTab] = useState<string | null>(null)
  const [appSettingsSection, setAppSettingsSection] = useState<'tabs' | undefined>(undefined)
  const [variablePrefill, setVariablePrefill] = useState<string | null>(null)
  const [variablePrefillType, setVariablePrefillType] = useState<DashboardVariableType | null>(null)

  const [containerWidth, setContainerWidth] = useState(0)
  const [deleteWidgetTarget, setDeleteWidgetTarget] = useState<string | null>(null)
  const isMobileLayout = containerWidth < 600
  const layoutContainerRef = useRef<HTMLDivElement | null>(null)

  // Observe the always-rendered body wrapper, not the grid: an empty
  // dashboard has no grid to mount, and measuring only the grid left the
  // width at 0 — so a brand-new dashboard on desktop read as "mobile" and
  // hid the desktop-only controls (App settings, publish). A mount-time
  // effect would run while the loading skeleton is up and never attach, so
  // the ref callback sets the width as soon as the element exists and the
  // ResizeObserver keeps it fresh on window resizes.
  const layoutObserverRef = useRef<ResizeObserver | null>(null)
  const layoutRef = useCallback((el: HTMLDivElement | null) => {
    layoutContainerRef.current = el
    layoutObserverRef.current?.disconnect()
    layoutObserverRef.current = null
    if (el) {
      setContainerWidth(el.clientWidth)
      const obs = new ResizeObserver(() => {
        // clientWidth (not entry.contentRect) so resize events report the
        // same box the initial measure and the layout effect use.
        setContainerWidth(el.clientWidth)
      })
      obs.observe(el)
      layoutObserverRef.current = obs
    }
  }, [])

  useLayoutEffect(() => {
    const el = layoutContainerRef.current
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
    staleTime: 0,
    refetchOnMount: true,
  })

  // Published-version status for the header chip. The endpoint requires
  // dashboard `edit`, so read-only visitors never poll it; it refreshes on
  // publish/restore invalidation and polls while the editor stays open.
  const versionsQuery = useDashboardVersions(id, dashboard?.can_edit !== false)
  const [showVersions, setShowVersions] = useState(false)
  const publishDashboard = useMutation({
    mutationFn: () => api.post(`/api/v1/dashboards/${id}/publish`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['dashboard-versions', id] })
      qc.invalidateQueries({ queryKey: ['dashboard', id] })
    },
    onError: (err: unknown) => setMutationError(publishErrorMessage(err)),
  })

  // Live dashboard document: connects as soon as the page mounts and becomes
  // the source of truth for title/settings/widgets once synced. Mutators arm
  // only after the document has synced and the relay is connected, so a
  // disconnected editor can never write to a stale local copy. (Adjusted
  // during render: the hook result is only known after it runs.)
  const [editingArmed, setEditingArmed] = useState(false)
  const canEdit = dashboard?.can_edit !== false
  const liveDoc = useDashboardDoc(id, { enabled: canEdit && editingArmed })
  const userEmail = localStorage.getItem('aether_user_email') ?? ''
  const { updateLayout: updateDocLayout, setConfig: setDocConfig } = liveDoc
  const editingEnabled = canEdit && liveDoc.synced && liveDoc.connected
  if (editingArmed !== editingEnabled) setEditingArmed(editingEnabled)

  const liveTitle = liveDoc.synced && liveDoc.title ? liveDoc.title : (dashboard?.title ?? '')

  useEffect(() => {
    document.title = liveTitle ? `${liveTitle} — Aether Notebooks` : 'Aether Notebooks'
    return () => { document.title = "Aether Notebooks" }
  }, [liveTitle])

  useEffect(() => {
    if (!dashboard) return
    const el = layoutContainerRef.current
    if (el) setContainerWidth(el.clientWidth)
  }, [dashboard])

  // Once the document has synced it is the source of truth for the widget
  // list; until then the REST payload paints the page.
  const widgets = useMemo(
    () => (liveDoc.synced ? liveDoc.widgets : (dashboard?.widgets ?? [])),
    [liveDoc.synced, liveDoc.widgets, dashboard?.widgets],
  )

  // Chapters: doc-sourced once synced. An empty list hides the strip's chips
  // and the grid filter (only the editor's add shortcut remains).
  const tabs = liveDoc.synced
    ? (liveDoc.settings.tabs ?? NO_TABS)
    : (dashboard?.settings?.tabs ?? NO_TABS)
  // A selected chapter that disappears (deleted by anyone) is forgotten so a
  // later tab reusing its id cannot silently re-apply the stale filter.
  if (editorTab !== null && !tabs.some((t) => t.id === editorTab)) setEditorTab(null)
  // The current render still sees the pre-clear value; fall back to All so the
  // grid never silently strands every widget off-screen.
  const activeEditorTab = editorTab && tabs.some((t) => t.id === editorTab) ? editorTab : null
  const shownWidgets = useMemo(
    () => (tabs.length === 0 || activeEditorTab === null
      ? widgets
      : widgets.filter((w: Widget) => widgetTabId(w, tabs) === activeEditorTab)),
    [widgets, tabs, activeEditorTab],
  )

  const { data: notebooks = [] } = useQuery({
    queryKey: ['notebooks'],
    queryFn: () => api.get<Notebook[]>('/api/v1/notebooks'),
    enabled: showPicker,
  })

  const { data: pickerNotebook } = useQuery({
    queryKey: ['notebook', pickerNotebookId],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${pickerNotebookId}`),
    enabled: !!pickerNotebookId,
  })

  const addWidget = useMutation({
    mutationFn: () => {
      const payload = buildAddWidgetPayload({
        source: pickerSource,
        type: pickerType,
        layout: nextWidgetLayout(widgets),
        notebookId: pickerNotebookId,
        cellId: pickerCellId,
        connectorId: pickerConnectorId,
        query: pickerQuery,
        markdown: pickerMarkdown,
      })
      return api.post<Widget>(`/api/v1/dashboards/${id}/widgets`, payload)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['dashboard', id] })
      setShowPicker(false)
      setPickerSource('cell')
      setPickerNotebookId('')
      setPickerCellId('')
      setPickerConnectorId(null)
      setPickerQuery('SELECT 1')
      setPickerType('table')
      setPickerMarkdown('')
      setPickerError(null)
    },
    onError: (err: Error) => setPickerError(err.message),
  })

  const deleteWidget = useMutation({
    mutationFn: (widgetId: string) =>
      api.delete(`/api/v1/dashboards/${id}/widgets/${widgetId}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dashboard', id] }),
    onError: (err: Error) => setMutationError(err.message),
  })

  // Chart config is a document field: write it straight to the shared doc
  // (the relay persists it) instead of a per-save REST round-trip.
  const saveWidgetConfig = useCallback((widgetId: string, config: ChartConfig) => {
    setDocConfig(widgetId, withWidgetOverride(config) as unknown as Record<string, unknown>)
  }, [setDocConfig])

  const resetWidgetConfig = useCallback((widgetId: string) => {
    setDocConfig(widgetId, {})
  }, [setDocConfig])

  // Drag/resize stops write the layout straight to the shared document (the
  // source of truth); the relay persists it and every replica sees the move
  // live. No REST call and no cache invalidation — the doc is live.
  //
  // `tabDrop` carries a chip drop: the dragged widget's new chapter is folded
  // into the same write as its position, so a chip drop is a single document
  // write per widget instead of a position write plus a tab write.
  const applyLayoutStop = useCallback((layout: Layout, tabDrop?: { widgetId: string; tab: string }) => {
    if (!editingEnabled) return
    // The compactor may have moved other widgets too: write every changed
    // widget's layout so overlapping changes merge instead of being lost.
    layout.forEach(item => {
      const widget = widgets.find((w: Widget) => w.id === item.i)
      if (!widget) return
      const prev = widget.layout
      const tab = tabDrop && tabDrop.widgetId === item.i ? tabDrop.tab : prev.tab
      if (prev.col === item.x && prev.row === item.y &&
          prev.width === item.w && prev.height === item.h &&
          (prev.tab ?? '') === (tab ?? '')) return
      updateDocLayout(item.i, { ...prev, row: item.y, col: item.x, width: item.w, height: item.h, tab })
    })
  }, [editingEnabled, widgets, updateDocLayout])

  const onResizeStop = useCallback((layout: Layout, _oldItem: LayoutItem | null, newItem: LayoutItem | null) => {
    if (!newItem || !layout) return
    applyLayoutStop(layout)
  }, [applyLayoutStop])

  const onDragStop = useCallback((
    layout: Layout,
    _oldItem: LayoutItem | null,
    newItem: LayoutItem | null,
    _placeholder: LayoutItem | null,
    e?: Event,
  ) => {
    if (!newItem || !layout) return
    // Dropping a card on a tab chip re-places it; the All chip (data-tab-id="")
    // clears the tab so the widget falls back to the first chapter.
    let tabDrop: { widgetId: string; tab: string } | undefined
    if (editingEnabled && tabs.length > 0 && e) {
      const tabId = tabIdAtPoint(e)
      if (tabId !== null) {
        const widget = widgets.find((w: Widget) => w.id === newItem.i)
        if (widget && (widget.layout?.tab ?? '') !== tabId) tabDrop = { widgetId: newItem.i, tab: tabId }
      }
    }
    // The library's compactor handles overlap prevention.
    applyLayoutStop(layout, tabDrop)
  }, [applyLayoutStop, editingEnabled, tabs, widgets])

  if (isLoading) {
    return (
      <AppShell>
        <div style={{ padding: '40px' }}>
          <Skeleton width={120} height={14} style={{ marginBottom: 16 }} />
          <Skeleton width={300} height={28} style={{ marginBottom: 24 }} />
          <Skeleton height={200} style={{ marginBottom: 16 }} />
          <Skeleton height={120} />
        </div>
      </AppShell>
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

  const pickerCells = pickerNotebook?.cells ?? []

  const pickerValidTypes = validTypesForSource(pickerSource)
  const pickerSourceComplete =
    pickerSource === 'cell'
      ? !!pickerNotebookId && !!pickerCellId
      : pickerSource === 'query'
        ? !!pickerConnectorId && !!pickerQuery.trim()
        : true
  const pickerTypeValid = pickerValidTypes.includes(pickerType)
  const pickerAddDisabled = addWidget.isPending || !pickerSourceComplete || !pickerTypeValid

  // Live-merged dashboard for the panels and header: when the doc has synced,
  // title/settings/variables come from it (the REST row is a derived cache).
  const liveDashboard: DashboardWithWidgets = liveDoc.synced
    ? {
        ...dashboard,
        title: liveTitle,
        settings: { ...dashboard.settings, ...liveDoc.settings, variables: liveDoc.variables },
      }
    : dashboard
  const displayTitle = liveDashboard.title
  const variables = liveDashboard.settings?.variables ?? []
  const gridCols = liveDashboard.settings?.grid_cols ?? 12

  // Status chip: the amber dot flags a published app that is behind the draft
  // (or a draft that publish would refuse). A never-published dashboard reads
  // "Not published" instead — a dot there would add nothing.
  const latestVersion = versionsQuery.data?.latest ?? null
  const versionWarnings = versionsQuery.data?.warnings ?? []
  const versionNeedsPublish = !!latestVersion && !!versionsQuery.data?.has_unpublished_changes
  const versionHasDot = versionNeedsPublish || versionWarnings.length > 0
  const versionChipTitle = versionWarnings.length > 0
    ? `Draft cannot be published: ${versionWarnings.join('; ')}`
    : versionNeedsPublish
      ? 'Unpublished changes'
      : 'Version history'

  return (
    <DashboardVariablesProvider dashboardId={id!} variables={variables}>
    <AppShell noPadding>
      {/* Sub-header */}
      <header style={{ ...styles.subHeader, ...(isMobileLayout ? styles.subHeaderMobile : {}) }}>
        <div style={styles.headerLeft}>
          <Link to="/dashboards" style={styles.backLink} title="Back to all dashboards">
            <ArrowLeft size={14} style={{ flexShrink: 0 }} />
            {!isMobileLayout && <span>Dashboards</span>}
          </Link>
          <span style={styles.breadcrumbSep}>/</span>
          {editingTitle ? (
            <input
              style={styles.titleInput}
              value={titleDraft}
              onChange={(e) => {
                setTitleDraft(e.target.value)
                // The title is a document field; the hook debounces the write.
                if (e.target.value.trim()) liveDoc.setTitle(e.target.value.trim())
              }}
              onBlur={() => setEditingTitle(false)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
                if (e.key === 'Escape') setEditingTitle(false)
              }}
              autoFocus
            />
          ) : (
            <span
              style={{ ...styles.dashboardTitle, cursor: editingEnabled ? 'pointer' : 'default' }}
              onClick={() => {
                if (!editingEnabled) return
                setTitleDraft(displayTitle)
                setEditingTitle(true)
              }}
              title={editingEnabled ? 'Click to rename' : undefined}
            >
              {displayTitle}
            </span>
          )}
        </div>
        <div style={styles.headerRight}>
          <CollaboratorAvatars awareness={liveDoc.awareness} currentUserEmail={userEmail} />
          {!isMobileLayout && (
            <div className="nav-seg" role="group" aria-label="Grid columns">
              <span style={{ fontSize: 10, color: 'var(--nav-text-muted)', fontWeight: 700, fontFamily: 'var(--font-mono)', textTransform: 'uppercase', letterSpacing: '0.08em', padding: '0 4px' }}>Cols</span>
              {[6, 8, 12, 16, 24].map(c => (
                <button
                  key={c}
                  type="button"
                  title={`${c} grid columns — ${c <= 8 ? 'compact' : c <= 12 ? 'standard' : 'wide'} layout`}
                  aria-label={`${c} columns`}
                  aria-pressed={gridCols === c}
                  disabled={!editingEnabled}
                  onClick={async () => {
                    if (!editingEnabled) return
                    markSaving()
                    const oldCols = gridCols
                    // Settings live in the document; the server merges this
                    // partial update into it and materializes the row.
                    await api.put(`/api/v1/dashboards/${id}`, {
                      settings: { grid_cols: c },
                    })
                    if (oldCols !== c) {
                      // Reflow layouts with the new column count so widgets
                      // stay inside the grid instead of overflowing the canvas.
                      // Layouts are document fields, so write them to the doc.
                      const updates = rescaleWidgetLayouts(widgets, oldCols, c)
                      updates.forEach(u => updateDocLayout(u.id, u.layout))
                    }
                    qc.invalidateQueries({ queryKey: ['dashboard', id] })
                    markSaved()
                  }}
                >
                  {c}
                </button>
              ))}
            </div>
          )}
          <button
            type="button"
            className="nav-btn"
            onClick={() => setShowVariables(true)}
            disabled={!editingEnabled}
            title={editingEnabled ? 'Manage dashboard variables and filters' : 'Editing is paused'}
          >
            Variables
          </button>
          {!isMobileLayout && (
            <button
              type="button"
              className="nav-btn"
              onClick={() => { setAppSettingsSection(undefined); setShowAppSettings(true) }}
              disabled={!editingEnabled}
              title={editingEnabled ? 'App settings' : 'Editing is paused'}
            >
              <LayoutTemplate size={12} /> App
            </button>
          )}
          {saveStatus && (
            <span style={{
              fontSize: 11, fontWeight: 600, color: saveStatus === 'saving' ? 'var(--nav-text-muted)' : 'var(--nav-text)',
              textTransform: 'uppercase', letterSpacing: '0.04em',
            }}>
              {saveStatus === 'saving' ? 'Saving…' : 'Saved'}
            </span>
          )}
          {/* Publish controls are editor-only (the versions API requires
              dashboard `edit`) and hidden on mobile like the App button. */}
          {!isMobileLayout && canEdit && (
            <>
              <button
                type="button"
                className="nav-btn"
                data-testid="publish-status"
                onClick={() => setShowVersions(true)}
                disabled={!editingEnabled}
                title={editingEnabled ? versionChipTitle : 'Editing is paused'}
              >
                {versionHasDot && <span data-testid="publish-status-dot" style={styles.publishDot} aria-hidden="true" />}
                {latestVersion ? `v${latestVersion.version}` : 'Not published'}
              </button>
              <button
                type="button"
                className="nav-btn"
                onClick={() => publishDashboard.mutate()}
                disabled={!editingEnabled || publishDashboard.isPending}
                title={editingEnabled ? 'Publish the current draft' : 'Editing is paused'}
              >
                {publishDashboard.isPending ? 'Publishing…' : 'Publish'}
              </button>
            </>
          )}
          <Link to={`/dashboards/${id}/view`} className="nav-btn" title="View dashboard">
            <Eye size={12} /> View
          </Link>
          <button
            type="button"
            className="nav-btn"
            onClick={() => setShowPermissions(true)}
            title="Manage permissions"
          >
            <Shield size={12} /> {!isMobileLayout && 'Permissions'}
          </button>
          <button
            type="button"
            style={{
              ...styles.addWidgetBtn,
              ...(isMobileLayout ? styles.addWidgetBtnMobile : {}),
              ...(editingEnabled ? {} : styles.disabledControl),
            }}
            onClick={() => setShowPicker(true)}
            disabled={!editingEnabled}
            title={editingEnabled ? 'Add Widget' : 'Editing is paused'}
          >
            {isMobileLayout ? <Plus size={16} /> : '+ Add Widget'}
          </button>
        </div>
      </header>

      {liveDoc.synced && !liveDoc.connected && (
        <ErrorBanner variant="warning" message="Reconnecting — editing is paused" />
      )}

      {mutationError && (
        <ErrorBanner message={mutationError} onDismiss={() => setMutationError(null)} />
      )}

      {/* Chapter strip (editor-local filter, not URL state): All + one chip per
          tab + the add shortcut that opens the App settings panel's Tabs
          section. Without tabs the strip collapses to the lone add shortcut so
          authors can create the first chapter on every layout (the header's
          App button is desktop-only); read-only visitors see no strip. */}
      {(tabs.length > 0 || editingEnabled) && (
        <div
          style={{ ...styles.tabStrip, ...(isMobileLayout ? styles.tabStripMobile : {}) }}
          role="group"
          aria-label="Dashboard chapters"
        >
          {tabs.length > 0 && (
            <button
              type="button"
              data-tab-id=""
              aria-pressed={activeEditorTab === null}
              style={activeEditorTab === null ? { ...styles.tabChip, ...styles.tabChipActive } : styles.tabChip}
              onClick={() => setEditorTab(null)}
            >
              All
            </button>
          )}
          {tabs.map((tab) => (
            <button
              key={tab.id}
              type="button"
              data-tab-id={tab.id}
              aria-pressed={activeEditorTab === tab.id}
              style={activeEditorTab === tab.id ? { ...styles.tabChip, ...styles.tabChipActive } : styles.tabChip}
              onClick={() => setEditorTab(tab.id)}
            >
              {tab.title}
            </button>
          ))}
          <button
            type="button"
            style={styles.tabAddBtn}
            aria-label="Add tab"
            title="Add a tab"
            disabled={!editingEnabled}
            onClick={() => {
              if (!editingEnabled) return
              setAppSettingsSection('tabs')
              setShowAppSettings(true)
            }}
          >
            ＋
          </button>
        </div>
      )}

      {/* Widget picker panel */}
      {showPicker && (
        <div style={styles.pickerOverlay}>
          <div style={styles.pickerPanel}>
            <div style={styles.pickerHeader}>
              <span style={styles.pickerTitle}>Add Widget</span>
              <button
                type="button"
                style={{ ...styles.pickerClose, display: 'flex', alignItems: 'center' }}
                onClick={() => {
                  setShowPicker(false)
                  setPickerSource('cell')
                  setPickerNotebookId('')
                  setPickerCellId('')
                  setPickerConnectorId(null)
                  setPickerQuery('SELECT 1')
                  setPickerType('table')
                  setPickerMarkdown('')
                  setPickerError(null)
                }}
              >
                <X size={15} />
              </button>
            </div>

            <div style={styles.pickerBody}>
              <label style={styles.pickerLabel}>Source</label>
              <select
                style={styles.pickerSelect}
                aria-label="Widget source"
                value={pickerSource}
                onChange={(e) => {
                  const source = e.target.value as AddWidgetSource
                  setPickerSource(source)
                  const valid = validTypesForSource(source)
                  if (!valid.includes(pickerType)) setPickerType(valid[0])
                }}
              >
                <option value="cell">Notebook cell</option>
                <option value="query">Query</option>
                <option value="none">None — text panel</option>
              </select>

              {pickerSource === 'cell' ? (
                <>
                  <label style={styles.pickerLabel}>Notebook</label>
                  <select
                    style={styles.pickerSelect}
                    value={pickerNotebookId}
                    onChange={(e) => {
                      setPickerNotebookId(e.target.value)
                      setPickerCellId('')
                    }}
                  >
                    <option value="">Select notebook…</option>
                    {notebooks.map((nb) => (
                      <option key={nb.id} value={nb.id}>{nb.title}</option>
                    ))}
                  </select>

                  <label style={styles.pickerLabel}>Cell</label>
                  <select
                    style={{ ...styles.pickerSelect, ...(!pickerNotebookId ? styles.pickerSelectDisabled : {}) }}
                    value={pickerCellId}
                    onChange={(e) => setPickerCellId(e.target.value)}
                    disabled={!pickerNotebookId}
                  >
                    <option value="">Select cell…</option>
                    {pickerCells.map((cell, i) => (
                      <option key={cell.id} value={cell.id}>
                        Cell {i + 1} ({cell.type}){cell.source ? ` — ${cell.source.slice(0, 40)}` : ''}
                      </option>
                    ))}
                  </select>
                </>
              ) : pickerSource === 'query' ? (
                <>
                  <label style={styles.pickerLabel}>Connector</label>
                  <ConnectorSelector value={pickerConnectorId} onChange={setPickerConnectorId} />
                  <label style={styles.pickerLabel}>SQL</label>
                  <SqlEditor value={pickerQuery} onChange={setPickerQuery} minHeight={120} />
                </>
              ) : (
                <>
                  <label style={styles.pickerLabel}>Markdown</label>
                  <textarea
                    style={styles.pickerTextarea}
                    aria-label="Text panel markdown"
                    placeholder={'## Title\n\nWrite markdown here…'}
                    value={pickerMarkdown}
                    onChange={(e) => setPickerMarkdown(e.target.value)}
                  />
                </>
              )}

              <label style={styles.pickerLabel}>Widget Type</label>
              <select
                style={{ ...styles.pickerSelect, ...(pickerSource === 'none' ? styles.pickerSelectDisabled : {}) }}
                aria-label="Widget type"
                value={pickerType}
                onChange={(e) => setPickerType(e.target.value as AddWidgetType)}
                disabled={pickerSource === 'none'}
              >
                <option value="table" disabled={!pickerValidTypes.includes('table')}>Table</option>
                <option value="chart" disabled={!pickerValidTypes.includes('chart')}>Chart</option>
                <option value="metric" disabled={!pickerValidTypes.includes('metric')}>Metric</option>
                <option value="text" disabled={!pickerValidTypes.includes('text')}>Text</option>
              </select>

              {pickerError && (
                <p style={styles.pickerError}>{pickerError}</p>
              )}

              <button
                type="button"
                style={{ ...styles.pickerAddBtn, ...(pickerAddDisabled || !editingEnabled ? styles.pickerAddBtnDisabled : {}) }}
                disabled={pickerAddDisabled || !editingEnabled}
                onClick={() => { if (editingEnabled) addWidget.mutate() }}
              >
                {addWidget.isPending ? 'Adding…' : 'Add Widget'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Main body */}
      <div ref={layoutRef} style={styles.body}>
        {variables.length > 0 && <DashboardVariableBar />}
        {widgets.length === 0 ? (
          <EmptyState
            title="No widgets yet"
            text="Add queries as tables, charts, or metrics — or write a text panel — to this dashboard."
            action={editingEnabled ? { label: '+ Add Widget', onClick: () => setShowPicker(true) } : undefined}
          />
        ) : (
          // The mobile/desktop branch is chosen only after the body width is
          // known (see layoutRef), otherwise the first render would mount the
          // mobile branch and the flip to desktop would remount every widget —
          // doubling its query fetches.
          <div style={{ minHeight: 240 }}>
            {shownWidgets.length === 0 ? (
              <p style={styles.emptyChapterNote}>No widgets in this chapter yet.</p>
            ) : containerWidth === 0 ? null : isMobileLayout ? (
              <div style={styles.mobileGrid}>
                {shownWidgets.map((widget: Widget) => (
                  <div key={widget.id} style={styles.mobileWidgetCard}>
                    <div className="dash-widget-head">
                      <span className="dash-widget-kind">{widgetKindLabel(widget)}</span>
                      <div className="dash-widget-head-actions">
                        <button
                          type="button"
                          className="dash-widget-ctl"
                          title="Edit widget"
                          aria-label={`Edit ${widgetDisplayName(widget)}`}
                          onClick={() => setEditingWidget(widget)}
                        >
                          <Pencil size={11} />
                        </button>
                        <button
                          type="button"
                          className="dash-widget-ctl"
                          title={editingEnabled ? 'Remove widget' : 'Editing is paused'}
                          aria-label={`Remove ${widgetDisplayName(widget)}`}
                          disabled={!editingEnabled}
                          onClick={() => setDeleteWidgetTarget(widget.id)}
                        >
                          <X size={12} />
                        </button>
                      </div>
                    </div>
                    <WidgetFrame appearance={appearanceFrom(widget.config)} style={styles.widgetFrame}>
                      {isQueryWidget(widget) ? (
                        <QueryDataWidget dashboardId={id!} widget={widget} canViewWithData={dashboard.can_view_with_data !== false} />
                      ) : isTextPanel(widget) ? (
                        <TextWidget markdown={textConfigFrom(widget.config).markdown} />
                      ) : (
                        <WidgetContent widget={widget} editingEnabled={editingEnabled} onConfigSave={saveWidgetConfig} onConfigReset={resetWidgetConfig} />
                      )}
                    </WidgetFrame>
                  </div>
                ))}
              </div>
            ) : (
              <GridLayout
                layout={shownWidgets.map(toGridItem)}
                width={containerWidth}
                gridConfig={{ cols: gridCols, rowHeight: 30, margin: [4, 4] }}
                // Whole-card drag: any non-interactive part of the widget moves
                // it. Buttons/links/fields and the chart canvas (tooltips,
                // dataZoom) are excluded. Drag/resize are off while editing is
                // paused (document not synced or relay disconnected).
                dragConfig={{ enabled: editingEnabled, cancel: 'button, a, input, select, textarea, canvas, .react-resizable-handle' }}
                resizeConfig={{ enabled: editingEnabled }}
                onResizeStop={onResizeStop}
                onDragStop={onDragStop}
                style={{ minHeight: 240 }}
              >
                {shownWidgets.map((widget: Widget) => (
                  <div key={widget.id} style={{ position: 'relative' }}>
                    <div className="widget-drag-handle dash-widget-drag" title="Drag to move" />
                    <div className="dash-widget-card" style={styles.widgetCard}>
                      <div className="dash-widget-head">
                        <span className="dash-widget-kind">{widgetKindLabel(widget)}</span>
                        <div className="dash-widget-head-actions">
                          <button
                            type="button"
                            className="dash-widget-ctl"
                            title="Edit widget"
                            aria-label={`Edit ${widgetDisplayName(widget)}`}
                            onClick={() => setEditingWidget(widget)}
                          >
                            <Pencil size={11} />
                          </button>
                          <button
                            type="button"
                            className="dash-widget-ctl"
                            title={editingEnabled ? 'Remove widget' : 'Editing is paused'}
                            aria-label={`Remove ${widgetDisplayName(widget)}`}
                            disabled={!editingEnabled}
                            onClick={() => setDeleteWidgetTarget(widget.id)}
                          >
                            <X size={12} />
                          </button>
                        </div>
                      </div>
                      <WidgetFrame appearance={appearanceFrom(widget.config)} style={styles.widgetFrame}>
                        {isQueryWidget(widget) ? (
                          <QueryDataWidget dashboardId={id!} widget={widget} canViewWithData={dashboard.can_view_with_data !== false} />
                        ) : isTextPanel(widget) ? (
                          <TextWidget markdown={textConfigFrom(widget.config).markdown} />
                        ) : (
                          <WidgetContent widget={widget} editingEnabled={editingEnabled} onConfigSave={saveWidgetConfig} onConfigReset={resetWidgetConfig} />
                        )}
                      </WidgetFrame>
                    </div>
                  </div>
                ))}
              </GridLayout>
            )}
          </div>
        )}
      </div>
      {showPermissions && (
        <PermissionsPanel
          resourceType="dashboard"
          resourceId={id!}
          resourceName={displayTitle}
          parentFolderId={undefined}
          resourceOwnerId={dashboard.created_by}
          onClose={() => setShowPermissions(false)}
        />
      )}
      <ConfirmDialog
        open={!!deleteWidgetTarget}
        title="Remove widget"
        message="Remove this widget from the dashboard?"
        confirmLabel="Remove"
        destructive
        onConfirm={() => {
          if (deleteWidgetTarget && editingEnabled) deleteWidget.mutate(deleteWidgetTarget)
          setDeleteWidgetTarget(null)
        }}
        onCancel={() => setDeleteWidgetTarget(null)}
      />
      {editingWidget && (
        <WidgetConfigDrawer
          key={editingWidget.id}
          dashboardId={id!}
          dashboard={liveDashboard}
          widget={editingWidget}
          closeOnEscape={!showVariables}
          editingEnabled={editingEnabled}
          docSynced={liveDoc.synced}
          onClose={() => setEditingWidget(null)}
          onSaved={() => qc.invalidateQueries({ queryKey: ['dashboard', id] })}
          onDefineVariable={(name, suggestedType) => {
            setVariablePrefill(name)
            setVariablePrefillType(suggestedType ?? null)
            setShowVariables(true)
          }}
        />
      )}
      {showVariables && (
        <DashboardVariablesPanel
          key={variablePrefill ?? 'variables'}
          dashboardId={id!}
          dashboard={liveDashboard}
          initialNewName={variablePrefill}
          initialNewType={variablePrefillType}
          onClose={() => { setShowVariables(false); setVariablePrefill(null); setVariablePrefillType(null) }}
          onSaved={() => qc.invalidateQueries({ queryKey: ['dashboard', id] })}
        />
      )}
      {showAppSettings && (
        <AppSettingsPanel
          dashboardId={id!}
          dashboard={liveDashboard}
          widgets={widgets}
          initialSection={appSettingsSection}
          onClose={() => setShowAppSettings(false)}
          onSaved={() => qc.invalidateQueries({ queryKey: ['dashboard', id] })}
        />
      )}
      {showVersions && (
        <DashboardVersionsPanel dashboardId={id!} onClose={() => setShowVersions(false)} />
      )}
    </AppShell>
    </DashboardVariablesProvider>
  )
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
    background: 'var(--border)',
  },
  subHeader: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    height: 44,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 12px',
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
    cursor: 'pointer',
  },
  titleInput: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--nav-text)',
    background: 'transparent',
    border: 'none',
    borderBottom: '1px solid var(--accent)',
    maxWidth: 400,
    fontFamily: 'var(--font-sans)',
    padding: '1px 2px',
  },
  headerRight: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    flexShrink: 0,
  },
  addWidgetBtn: {
    padding: '5px 12px',
    background: 'var(--button-primary-bg)',
    color: 'var(--button-primary-text)',
    border: 'none',
    borderRadius: 4,
    fontSize: 12,
    fontWeight: 600,
    cursor: 'pointer',
  },
  disabledControl: {
    opacity: 0.5,
    cursor: 'not-allowed',
  },
  // Amber "published app is behind the draft" dot on the status chip.
  publishDot: {
    width: 7,
    height: 7,
    borderRadius: '50%',
    background: 'var(--warning)',
    flexShrink: 0,
  },
  pickerOverlay: {
    position: 'fixed',
    inset: 0,
    background: 'var(--bg-overlay)',
    zIndex: 200,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  pickerPanel: {
    background: 'var(--bg-card)',
    borderLeft: '1px solid var(--border)',
    borderRadius: 0,
    width: 400,
    maxWidth: '90vw',
  },
  pickerHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 20px',
    borderBottom: '1px solid var(--border)',
    background: 'var(--bg-card)',
  },
  pickerTitle: {
    fontSize: 15,
    fontWeight: 700,
    color: 'var(--text-primary)',
  },
  pickerClose: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 15,
    padding: '2px 4px',
    borderRadius: 4,
  },
  pickerBody: {
    padding: '20px',
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
  pickerLabel: {
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
  },
  pickerSelect: {
    width: '100%',
    padding: '8px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
  },
  pickerSelectDisabled: {
    opacity: 0.5,
  },
  pickerTextarea: {
    width: '100%',
    minHeight: 120,
    padding: '8px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
    lineHeight: 1.6,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    resize: 'vertical',
    boxSizing: 'border-box',
  },
  pickerError: {
    color: 'var(--error)',
    fontSize: 12,
    margin: 0,
  },
  pickerAddBtn: {
    padding: '9px 0',
    background: 'var(--button-primary-bg)',
    color: 'var(--button-primary-text)',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    marginTop: 4,
  },
  pickerAddBtnDisabled: {
    opacity: 0.5,
    cursor: 'not-allowed',
  },
  subHeaderMobile: {
    height: 'auto',
    minHeight: 52,
    padding: '8px 16px',
    flexWrap: 'wrap',
    gap: 8,
  },
  // The editor chapter strip: quiet chips under the sub-header. Chips carry
  // `data-tab-id` (All carries "") so a card dropped on one is re-placed.
  tabStrip: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '6px 12px',
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    flexShrink: 0,
    overflowX: 'auto',
    position: 'sticky',
    top: 44,
    zIndex: 98,
  },
  tabChip: {
    padding: '3px 12px',
    background: 'rgba(255,255,255,0.06)',
    color: 'var(--nav-text-muted)',
    border: '1px solid transparent',
    borderRadius: 999,
    fontSize: 12,
    fontWeight: 500,
    cursor: 'pointer',
    whiteSpace: 'nowrap',
    flexShrink: 0,
  },
  tabChipActive: {
    background: 'var(--accent)',
    borderColor: 'var(--accent)',
    color: 'var(--button-primary-text, #fff)',
    fontWeight: 600,
  },
  tabAddBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 22,
    height: 22,
    padding: 0,
    background: 'rgba(255,255,255,0.06)',
    color: 'var(--nav-text-muted)',
    border: '1px dashed var(--nav-border)',
    borderRadius: 999,
    fontSize: 13,
    lineHeight: 1,
    cursor: 'pointer',
    flexShrink: 0,
  },
  // The mobile sub-header wraps to its own height, so the strip stays in flow
  // instead of sticky-positioning under a fixed header height.
  tabStripMobile: {
    position: 'relative',
    top: 'auto',
  },
  emptyChapterNote: {
    margin: 0,
    padding: '24px 0',
    fontSize: 13,
    fontStyle: 'italic',
    color: 'var(--text-muted)',
  },
  addWidgetBtnMobile: {
    padding: '5px 12px',
  },
  mobileGrid: {
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
  // The widget's card chrome (background/border) is painted by the
  // WidgetFrame inside, so the editor shell stays transparent and the frame
  // is the single source of card chrome — otherwise a `transparent`
  // appearance could not preview.
  mobileWidgetCard: {
    background: 'transparent',
    border: 'none',
    borderRadius: 4,
    overflow: 'hidden',
    position: 'relative',
  },
  body: {
    flex: 1,
    margin: 0,
    padding: '8px 12px',
    width: '100%',
  },
  // Transparent shell around the editor's head bar (kind badge + actions)
  // and the widget frame; the hover outline still marks the editable unit.
  // See mobileWidgetCard.
  widgetCard: {
    background: 'transparent',
    border: 'none',
    borderRadius: 4,
    overflow: 'hidden',
    position: 'relative',
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
  },
  // Fills the space below the head bar (desktop) / the card's content box
  // (mobile). The -1px top pull overlaps the head bar's border-bottom with
  // the frame's border-top so the two hairlines read as one seam.
  widgetFrame: {
    flex: 1,
    minHeight: 0,
    marginTop: -1,
  },
}
