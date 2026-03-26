import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard, Notebook, Cell, Widget } from '../types'
import { AppShell } from '../components/AppShell'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

interface DashboardWithWidgets extends Dashboard {
  widgets: Widget[]
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
            <span style={styles.backArrow}>←</span>
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
        <div style={styles.errorBanner}>
          {mutationError}
          <button
            type="button"
            onClick={() => setMutationError(null)}
            style={styles.errorClose}
          >
            ✕
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
                style={styles.pickerClose}
                onClick={() => {
                  setShowPicker(false)
                  setPickerNotebookId('')
                  setPickerCellId('')
                  setPickerType('table')
                  setPickerError(null)
                }}
              >
                ✕
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
          <div style={styles.empty}>
            <p style={styles.emptyText}>No widgets yet</p>
            <p style={styles.emptySubtext}>
              Add widgets to display notebook cell outputs in this dashboard.
            </p>
            <button
              type="button"
              style={styles.addWidgetBtn}
              onClick={() => setShowPicker(true)}
            >
              + Add Widget
            </button>
          </div>
        ) : (
          <div style={styles.grid}>
            {widgets.map((widget) => {
              const notebookLabel = widget.notebook_id.slice(0, 8)
              const cellLabel = widget.cell_id.slice(0, 8)
              return (
                <div key={widget.id} style={styles.widgetCard}>
                  <div style={styles.widgetHeader}>
                    <span style={styles.widgetTypeBadge}>{widget.type}</span>
                    <button
                      type="button"
                      style={styles.deleteWidgetBtn}
                      title="Remove widget"
                      onClick={() => {
                        if (confirm('Remove this widget?')) {
                          deleteWidget.mutate(widget.id)
                        }
                      }}
                    >
                      ✕
                    </button>
                  </div>
                  <div style={styles.widgetBody}>
                    <div style={styles.widgetRef}>
                      <span style={styles.widgetRefLabel}>Notebook</span>
                      <code style={styles.widgetRefValue}>{notebookLabel}…</code>
                    </div>
                    <div style={styles.widgetRef}>
                      <span style={styles.widgetRefLabel}>Cell</span>
                      <code style={styles.widgetRefValue}>{cellLabel}…</code>
                    </div>
                  </div>
                </div>
              )
            })}
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
    background: 'var(--accent)',
    opacity: 0.5,
  },
  subHeader: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    height: 52,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 24px',
    flexShrink: 0,
    position: 'sticky',
    top: 56,
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
    color: '#6a6260',
    textDecoration: 'none',
    fontSize: 13,
    fontWeight: 500,
    flexShrink: 0,
  },
  backArrow: {
    fontSize: 16,
    lineHeight: 1,
  },
  breadcrumbSep: {
    color: '#3a3630',
    fontSize: 14,
    flexShrink: 0,
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
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
  errorBanner: {
    background: '#fff0f0',
    borderBottom: '1px solid #fcd0d0',
    padding: '6px 24px',
    fontSize: 12,
    color: '#c0392b',
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  errorClose: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: '#c0392b',
    fontSize: 14,
    padding: 0,
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
    background: 'white',
    borderRadius: 12,
    boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
    width: 400,
    maxWidth: '90vw',
  },
  pickerHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 20px',
    borderBottom: '1px solid var(--border)',
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
    borderRadius: 7,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
    background: 'white',
    color: 'var(--text-primary)',
  },
  pickerError: {
    color: 'var(--error)',
    fontSize: 12,
    margin: 0,
  },
  pickerAddBtn: {
    padding: '9px 0',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 7,
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
  empty: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: 300,
    gap: 8,
  },
  emptyText: {
    fontSize: 16,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    margin: 0,
  },
  emptySubtext: {
    fontSize: 13,
    color: 'var(--text-muted)',
    margin: '0 0 16px',
  },
  grid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(2, 1fr)',
    gap: 16,
  },
  widgetCard: {
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 10,
    boxShadow: 'var(--shadow-sm)',
    overflow: 'hidden',
  },
  widgetHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '10px 14px',
    borderBottom: '1px solid var(--border)',
    background: 'var(--bg-secondary)',
  },
  widgetTypeBadge: {
    fontSize: 11,
    fontWeight: 700,
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
    color: 'var(--accent)',
    background: '#f0edff',
    padding: '2px 8px',
    borderRadius: 4,
  },
  deleteWidgetBtn: {
    background: 'transparent',
    border: 'none',
    fontSize: 13,
    cursor: 'pointer',
    color: 'var(--text-muted)',
    padding: '3px 6px',
    borderRadius: 4,
  },
  widgetBody: {
    padding: '14px 16px',
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  widgetRef: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  widgetRefLabel: {
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-muted)',
    textTransform: 'uppercase',
    letterSpacing: '0.04em',
    width: 60,
    flexShrink: 0,
  },
  widgetRefValue: {
    fontSize: 12,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
    background: 'var(--bg-secondary)',
    padding: '2px 6px',
    borderRadius: 4,
  },
}
