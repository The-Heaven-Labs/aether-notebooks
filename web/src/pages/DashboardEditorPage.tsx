import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft, X } from 'lucide-react'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard, Notebook, Cell, Widget } from '../types'
import { OutputRenderer } from '../components/OutputRenderer'
import { ErrorBanner } from '../components/ErrorBanner'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

interface DashboardWithWidgets extends Dashboard {
  widgets: Widget[]
}

function WidgetContent({ widget }: { widget: Widget }) {
  const { data: notebook, isLoading } = useQuery({
    queryKey: ['notebook', widget.notebook_id],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${widget.notebook_id}`),
  })

  if (isLoading) return <div style={widgetContentStyles.loading}>Loading…</div>

  const cell = notebook?.cells?.find((c: Cell) => c.id === widget.cell_id)
  if (!cell) return <div style={widgetContentStyles.empty}>Cell not found</div>
  if (!cell.outputs?.length) {
    return <div style={widgetContentStyles.empty}>No results yet — run the notebook first</div>
  }
  const fixedView = widget.type === 'chart' ? 'chart' : 'table'
  return <OutputRenderer outputs={cell.outputs} fixedView={fixedView} />
}

const widgetContentStyles: Record<string, React.CSSProperties> = {
  loading: { padding: '16px', fontSize: 13, color: 'var(--text-muted)' },
  empty: { padding: '16px', fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
}

export function DashboardEditorPage() {
  const { id } = useParams<{ id: string }>()
  const qc = useQueryClient()

  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [mutationError, setMutationError] = useState<string | null>(null)

  const [showPicker, setShowPicker] = useState(false)
  const [pickerNotebookId, setPickerNotebookId] = useState('')
  const [pickerCellId, setPickerCellId] = useState('')
  const [pickerType, setPickerType] = useState<'table' | 'chart'>('table')
  const [pickerError, setPickerError] = useState<string | null>(null)

  const { data: dashboard, isLoading, error } = useQuery({
    queryKey: ['dashboard', id],
    queryFn: () => api.get<DashboardWithWidgets>(`/api/v1/dashboards/${id}`),
    enabled: !!id,
  })

  useEffect(() => {
    if (dashboard) document.title = `${dashboard.title} — Heaven's Notebooks`
    return () => { document.title = "Heaven's Notebooks" }
  }, [dashboard])

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

  const renameBoard = useMutation({
    mutationFn: (title: string) =>
      api.put(`/api/v1/dashboards/${id}`, { title }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dashboard', id] }),
    onError: (err: Error) => setMutationError(err.message),
  })

  const addWidget = useMutation({
    mutationFn: () =>
      api.post<Widget>(`/api/v1/dashboards/${id}/widgets`, {
        notebook_id: pickerNotebookId,
        cell_id: pickerCellId,
        type: pickerType,
        layout: { x: 0, y: 0, w: 1, h: 1 },
        config: {},
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['dashboard', id] })
      setShowPicker(false)
      setPickerNotebookId('')
      setPickerCellId('')
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

  const widgets = dashboard.widgets ?? []
  const pickerCells = pickerNotebook?.cells ?? []

  return (
    <AppShell>
      {/* Sub-header */}
      <header style={styles.subHeader}>
        <div style={styles.headerLeft}>
          <Link to="/dashboards" style={styles.backLink}>
            <ArrowLeft size={14} style={{ flexShrink: 0 }} />
            <span>Dashboards</span>
          </Link>
          <span style={styles.breadcrumbSep}>/</span>
          {editingTitle ? (
            <input
              style={styles.titleInput}
              value={titleDraft}
              onChange={(e) => setTitleDraft(e.target.value)}
              onBlur={() => {
                setEditingTitle(false)
                if (titleDraft.trim() && titleDraft.trim() !== dashboard.title) {
                  renameBoard.mutate(titleDraft.trim())
                }
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
                if (e.key === 'Escape') setEditingTitle(false)
              }}
              autoFocus
            />
          ) : (
            <span
              style={styles.dashboardTitle}
              onClick={() => { setTitleDraft(dashboard.title); setEditingTitle(true) }}
              title="Click to rename"
            >
              {dashboard.title}
            </span>
          )}
        </div>
        <div style={styles.headerRight}>
          <button
            type="button"
            style={styles.addWidgetBtn}
            onClick={() => setShowPicker(true)}
          >
            + Add Widget
          </button>
        </div>
      </header>

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
                  setPickerNotebookId('')
                  setPickerCellId('')
                  setPickerType('table')
                  setPickerError(null)
                }}
              >
                <X size={15} />
              </button>
            </div>

            <div style={styles.pickerBody}>
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
                disabled={!pickerNotebookId || !pickerCellId || addWidget.isPending}
                onClick={() => addWidget.mutate()}
              >
                {addWidget.isPending ? 'Adding…' : 'Add Widget'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Main body */}
      <div style={styles.body}>
        {widgets.length === 0 ? (
          <EmptyState
            title="No widgets yet"
            text="Add widgets to display notebook cell outputs in this dashboard."
            action={{ label: '+ Add Widget', onClick: () => setShowPicker(true) }}
          />
        ) : (
          <div style={styles.grid}>
            {widgets.map((widget) => (
                <div key={widget.id} style={styles.widgetCard}>
                  <button
                    type="button"
                    style={{ ...styles.deleteWidgetBtn, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                    title="Remove widget"
                    onClick={() => {
                      if (confirm('Remove this widget?')) {
                        deleteWidget.mutate(widget.id)
                      }
                    }}
                  >
                    <X size={12} />
                  </button>
                  <WidgetContent widget={widget} />
                </div>
              ))}
          </div>
        )}
      </div>
    </AppShell>
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
    background: '#ddd',
  },
  subHeader: {
    background: 'var(--bg-primary)',
    borderBottom: '1px solid var(--border)',
    height: 52,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 24px',
    flexShrink: 0,
    position: 'sticky',
    top: -32,        // negate AppShell main's 32px padding so bar sticks at top edge
    zIndex: 99,
    // escape AppShell's 32px padding on all three sides
    margin: '-32px -32px 0',
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
    color: 'var(--text-muted)',
    textDecoration: 'none',
    fontSize: 13,
    fontWeight: 500,
    flexShrink: 0,
  },
  breadcrumbSep: {
    color: 'var(--text-muted)',
    fontSize: 14,
    flexShrink: 0,
  },
  dashboardTitle: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--text-primary)',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    maxWidth: 400,
    cursor: 'pointer',
  },
  titleInput: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--text-primary)',
    background: 'transparent',
    border: 'none',
    borderBottom: '1px solid var(--accent)',
    outline: 'none',
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
    padding: '6px 16px',
    background: '#111',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
  pickerOverlay: {
    position: 'fixed',
    inset: 0,
    background: 'rgba(0,0,0,0.35)',
    zIndex: 200,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  pickerPanel: {
    background: '#fff',
    borderLeft: '1px solid #e8e8e8',
    borderRadius: 0,
    width: 400,
    maxWidth: '90vw',
  },
  pickerHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 20px',
    borderBottom: '1px solid #e8e8e8',
    background: '#fff',
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
    border: '1px solid #ddd',
    borderRadius: 4,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
    background: '#fff',
    color: 'var(--text-primary)',
  },
  pickerError: {
    color: 'var(--error)',
    fontSize: 12,
    margin: 0,
  },
  pickerAddBtn: {
    padding: '9px 0',
    background: '#111',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    marginTop: 4,
  },
  body: {
    flex: 1,
    maxWidth: 1280,
    margin: '0 auto',
    padding: '40px 40px',
    width: '100%',
  },
  grid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(2, 1fr)',
    gap: 16,
  },
  widgetCard: {
    background: '#fff',
    border: '1px solid #e8e8e8',
    borderRadius: 4,
    overflow: 'hidden',
    position: 'relative',
  },
  deleteWidgetBtn: {
    position: 'absolute',
    top: 8,
    right: 8,
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    cursor: 'pointer',
    color: 'var(--text-muted)',
    padding: '2px 6px',
    lineHeight: 1,
    zIndex: 1,
    opacity: 0.7,
  },
}
