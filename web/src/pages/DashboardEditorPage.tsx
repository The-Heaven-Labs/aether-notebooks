import { useState, useEffect, useRef, useCallback, useLayoutEffect, useMemo } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft, X, Plus, Eye, Pencil, Shield } from 'lucide-react'
import { AppShell } from '../components/AppShell'
import { DashboardVariablesProvider } from '../contexts/DashboardVariablesContext'
import { DashboardVariableBar } from '../components/DashboardVariableBar'
import { rescaleWidgetLayouts } from '../utils/dashboardGrid'
import { EmptyState } from '../components/EmptyState'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard, DashboardVariableType, Notebook, Cell, Widget } from '../types'
import type { ChartConfig } from '../charts/types'
import { mergeWidgetChartConfig, hasWidgetOverride, withWidgetOverride } from '../charts/widgetChartConfig'
import { OutputRenderer } from '../components/OutputRenderer'
import { QueryDataWidget } from '../components/QueryDataWidget'
import { ErrorBanner } from '../components/ErrorBanner'
import { GridLayout } from 'react-grid-layout'
import type { LayoutItem, Layout } from 'react-grid-layout'
import { Skeleton } from '../components/Skeleton'
import { CollaboratorAvatars } from '../components/CollaboratorAvatars'
import { PermissionsPanel } from '../components/PermissionsPanel'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { WidgetConfigDrawer } from '../components/WidgetConfigDrawer'
import { DashboardVariablesPanel } from '../components/DashboardVariablesPanel'
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

function nextWidgetLayout(widgets: Widget[]): { row: number; col: number; width: number; height: number } {
  if (!widgets.length) return { row: 0, col: 0, width: 6, height: 8 }
  const maxBottom = widgets.reduce((max, w) => Math.max(max, w.layout.row + w.layout.height), 0)
  return { row: maxBottom, col: 0, width: 6, height: 8 }
}

function isQueryWidget(w: Widget): boolean {
  return !!w.connector_id && !!w.query
}

/** Human name for a widget's controls; falls back to a generic label. */
function widgetDisplayName(widget: Widget): string {
  const cfg = widget.config as Record<string, unknown> | undefined
  const t = typeof cfg?.title === 'string' ? cfg.title : typeof cfg?.label === 'string' ? cfg.label : ''
  return t || 'widget'
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
  const [pickerSource, setPickerSource] = useState<'cell' | 'query'>('cell')
  const [pickerNotebookId, setPickerNotebookId] = useState('')
  const [pickerCellId, setPickerCellId] = useState('')
  const [pickerConnectorId, setPickerConnectorId] = useState<string | null>(null)
  const [pickerQuery, setPickerQuery] = useState('SELECT 1')
  const [pickerType, setPickerType] = useState<'table' | 'chart'>('table')
  const [pickerError, setPickerError] = useState<string | null>(null)
  const [editingWidget, setEditingWidget] = useState<Widget | null>(null)
  const [showVariables, setShowVariables] = useState(false)
  const [variablePrefill, setVariablePrefill] = useState<string | null>(null)
  const [variablePrefillType, setVariablePrefillType] = useState<DashboardVariableType | null>(null)

  const [containerWidth, setContainerWidth] = useState(0)
  const [deleteWidgetTarget, setDeleteWidgetTarget] = useState<string | null>(null)
  const isMobileLayout = containerWidth < 600
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
    staleTime: 0,
    refetchOnMount: true,
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
    const el = gridContainerRef.current
    if (el) setContainerWidth(el.clientWidth)
  }, [dashboard])

  // Once the document has synced it is the source of truth for the widget
  // list; until then the REST payload paints the page.
  const widgets = useMemo(
    () => (liveDoc.synced ? liveDoc.widgets : (dashboard?.widgets ?? [])),
    [liveDoc.synced, liveDoc.widgets, dashboard?.widgets],
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
      const base = {
        type: pickerType,
        layout: nextWidgetLayout(widgets),
        config: {},
      }
      const payload = pickerSource === 'query'
        ? { ...base, connector_id: pickerConnectorId, query: pickerQuery, language: 'sql' }
        : { ...base, notebook_id: pickerNotebookId, cell_id: pickerCellId }
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
  const applyLayoutStop = useCallback((layout: Layout) => {
    if (!editingEnabled) return
    // The compactor may have moved other widgets too: write every changed
    // widget's layout so overlapping changes merge instead of being lost.
    layout.forEach(item => {
      const widget = widgets.find((w: Widget) => w.id === item.i)
      if (!widget) return
      const prev = widget.layout
      if (prev.col === item.x && prev.row === item.y &&
          prev.width === item.w && prev.height === item.h) return
      updateDocLayout(item.i, { row: item.y, col: item.x, width: item.w, height: item.h })
    })
  }, [editingEnabled, widgets, updateDocLayout])

  const onResizeStop = useCallback((layout: Layout, _oldItem: LayoutItem | null, newItem: LayoutItem | null) => {
    if (!newItem || !layout) return
    applyLayoutStop(layout)
  }, [applyLayoutStop])

  const onDragStop = useCallback((layout: Layout, _oldItem: LayoutItem | null, newItem: LayoutItem | null) => {
    if (!newItem || !layout) return
    // The library's compactor handles overlap prevention.
    applyLayoutStop(layout)
  }, [applyLayoutStop])

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
          {saveStatus && (
            <span style={{
              fontSize: 11, fontWeight: 600, color: saveStatus === 'saving' ? 'var(--nav-text-muted)' : 'var(--nav-text)',
              textTransform: 'uppercase', letterSpacing: '0.04em',
            }}>
              {saveStatus === 'saving' ? 'Saving…' : 'Saved'}
            </span>
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
                onChange={(e) => setPickerSource(e.target.value as 'cell' | 'query')}
              >
                <option value="cell">Notebook cell</option>
                <option value="query">Query</option>
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
                    style={styles.pickerSelect}
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
              ) : (
                <>
                  <label style={styles.pickerLabel}>Connector</label>
                  <ConnectorSelector value={pickerConnectorId} onChange={setPickerConnectorId} />
                  <label style={styles.pickerLabel}>SQL</label>
                  <SqlEditor value={pickerQuery} onChange={setPickerQuery} minHeight={120} />
                </>
              )}

              <label style={styles.pickerLabel}>Widget Type</label>
              <select
                style={styles.pickerSelect}
                value={pickerType}
                onChange={(e) => setPickerType(e.target.value as 'table' | 'chart')}
              >
                <option value="table">Table</option>
                <option value="chart">Chart</option>
              </select>

              {pickerError && (
                <p style={styles.pickerError}>{pickerError}</p>
              )}

              <button
                type="button"
                style={styles.pickerAddBtn}
                disabled={
                  !editingEnabled ||
                  addWidget.isPending ||
                  (pickerSource === 'cell'
                    ? !pickerNotebookId || !pickerCellId
                    : !pickerConnectorId || !pickerQuery.trim())
                }
                onClick={() => { if (editingEnabled) addWidget.mutate() }}
              >
                {addWidget.isPending ? 'Adding…' : 'Add Widget'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Main body */}
      <div style={styles.body}>
        {variables.length > 0 && <DashboardVariableBar />}
        {widgets.length === 0 ? (
          <EmptyState
            title="No widgets yet"
            text="Add widgets to display notebook cell outputs in this dashboard."
            action={editingEnabled ? { label: '+ Add Widget', onClick: () => setShowPicker(true) } : undefined}
          />
        ) : (
          // One persistent measured wrapper: the mobile/desktop branch is
          // chosen only after the width is known, otherwise the first render
          // (width 0) mounts the mobile branch and the flip to desktop
          // remounts every widget — doubling its query fetches.
          <div ref={gridRef} style={{ minHeight: 240 }}>
            {containerWidth === 0 ? null : isMobileLayout ? (
              <div style={styles.mobileGrid}>
                {widgets.map((widget: Widget) => (
                  <div key={widget.id} style={styles.mobileWidgetCard}>
                    <div className="dash-widget-head">
                      <span className="dash-widget-kind">{isQueryWidget(widget) ? 'Query' : 'Cell'}</span>
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
                    <div className="dash-widget-data">
                      {isQueryWidget(widget) ? (
                        <QueryDataWidget dashboardId={id!} widget={widget} canViewWithData={dashboard.can_view_with_data !== false} />
                      ) : (
                        <WidgetContent widget={widget} editingEnabled={editingEnabled} onConfigSave={saveWidgetConfig} onConfigReset={resetWidgetConfig} />
                      )}
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <GridLayout
                layout={widgets.map(toGridItem)}
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
                {widgets.map((widget: Widget) => (
                  <div key={widget.id} style={{ position: 'relative' }}>
                    <div className="widget-drag-handle dash-widget-drag" title="Drag to move" />
                    <div className="dash-widget-card" style={styles.widgetCard}>
                      <div className="dash-widget-head">
                        <span className="dash-widget-kind">{isQueryWidget(widget) ? 'Query' : 'Cell'}</span>
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
                      <div className="dash-widget-data">
                        {isQueryWidget(widget) ? (
                          <QueryDataWidget dashboardId={id!} widget={widget} canViewWithData={dashboard.can_view_with_data !== false} />
                        ) : (
                          <WidgetContent widget={widget} editingEnabled={editingEnabled} onConfigSave={saveWidgetConfig} onConfigReset={resetWidgetConfig} />
                        )}
                      </div>
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
  subHeaderMobile: {
    height: 'auto',
    minHeight: 52,
    padding: '8px 16px',
    flexWrap: 'wrap',
    gap: 8,
  },
  addWidgetBtnMobile: {
    padding: '5px 12px',
  },
  mobileGrid: {
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
  mobileWidgetCard: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
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
  widgetCard: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border-light)',
    borderRadius: 4,
    overflow: 'hidden',
    position: 'relative',
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
  },
}
