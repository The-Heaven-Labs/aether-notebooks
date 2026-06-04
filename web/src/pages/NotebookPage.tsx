import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ChevronsRight, ChevronLeft, Loader2, X, Bot, Check } from 'lucide-react'
import { AppShell } from '../components/AppShell'
import { Skeleton } from '../components/Skeleton'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Notebook, Cell, Output, Connector, Parameter, CellVersion, Dashboard, Widget } from '../types'
import { Cell as NotebookCell } from '../components/Cell'
import { ParametersBar } from '../components/ParametersBar'
import { SchemaBrowser } from '../components/SchemaBrowser'
import { SchedulesPanel } from '../components/SchedulesPanel'
import { useNotebookKeyboardShortcuts } from '../hooks/useNotebookKeyboardShortcuts'
import { HistoryPanel } from '../components/HistoryPanel'
import { ShortcutsModal } from '../components/ShortcutsModal'
import { ConnectorSelector } from '../components/ConnectorSelector'
import { ErrorBanner } from '../components/ErrorBanner'
import { AgentPanel } from '../components/AgentPanel'
import { useNotebookWs } from '../hooks/useNotebookWs'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

function fmtTime(date: Date): string {
  const now = Date.now()
  const diffMs = now - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)

  if (diffSec < 60) return 'Just now'
  if (diffMin < 60) return `${diffMin} minute${diffMin !== 1 ? 's' : ''} ago`
  if (diffHour < 24) return `${diffHour} hour${diffHour !== 1 ? 's' : ''} ago`
  if (diffDay < 7) return `${diffDay} day${diffDay !== 1 ? 's' : ''} ago`
  return date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

function AddCellBar({ onAddCode, onAddText }: { onAddCode: () => void; onAddText: () => void }) {
  const [hovered, setHovered] = useState(false)
  return (
    <div
      style={{
        ...addBarStyles.bar,
        ...(hovered ? addBarStyles.barHover : {}),
      }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <div style={addBarStyles.line} />
      <div style={{ ...addBarStyles.buttons, opacity: hovered ? 1 : 0 }}>
        <button style={addBarStyles.btn} onClick={onAddCode}>+ Code</button>
        <button style={addBarStyles.btn} onClick={onAddText}>+ Text</button>
      </div>
      <div style={addBarStyles.line} />
    </div>
  )
}

const addBarStyles: Record<string, React.CSSProperties> = {
  bar: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    height: 20,
    margin: '2px 0',
    transition: 'all 0.15s ease',
    cursor: 'pointer',
  },
  barHover: {
    height: 28,
    margin: '4px 0',
  },
  line: {
    flex: 1,
    height: 1,
    background: 'var(--border-light)',
    transition: 'background 0.15s',
  },
  buttons: {
    display: 'flex',
    gap: 6,
    transition: 'opacity 0.12s',
    flexShrink: 0,
  },
  btn: {
    padding: '2px 10px',
    border: '1px dashed var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
    color: 'var(--text-muted)',
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    cursor: 'pointer',
    lineHeight: 1.4,
  },
}

export function NotebookPage() {
  const { id } = useParams<{ id: string }>()
  const qc = useQueryClient()
  const [runningCells, setRunningCells] = useState<Set<string>>(new Set())
  const [localCells, setLocalCells] = useState<Cell[]>([])
  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [descDraft, setDescDraft] = useState('')
  const paramStorageKey = `hnb_params_${id}`
  const [paramValues, setParamValues] = useState<Record<string, string>>(() => {
    try { return JSON.parse(localStorage.getItem(paramStorageKey) ?? '{}') } catch { return {} }
  })
  const [showSchema, setShowSchema] = useState(false)
  const [showSchedules, setShowSchedules] = useState(false)
  const [showParameters, setShowParameters] = useState(false)
  const [notebookConnectorId, setNotebookConnectorId] = useState<string>('')
  const [mutationError, setMutationError] = useState<string | null>(null)
  const [cellSaveState, setCellSaveState] = useState<Record<string, { saving: boolean; savedAt: Date | null; error: string | null }>>({})

  // Derive global save status from per-cell save states
  const latestCellSave = useMemo(() => {
    let latest: Date | null = null
    for (const state of Object.values(cellSaveState)) {
      if (state.savedAt && (!latest || state.savedAt > latest)) {
        latest = state.savedAt
      }
    }
    return latest
  }, [cellSaveState])

  const anyCellSaving = useMemo(() =>
    Object.values(cellSaveState).some(s => s.saving),
    [cellSaveState]
  )

  const anyCellError = useMemo(() =>
    Object.values(cellSaveState).some(s => s.error),
    [cellSaveState]
  )

  const [cellRunAt, setCellRunAt] = useState<Record<string, Date>>({})
  const [focusedCellId, setFocusedCellId] = useState<string | null>(null)
  // isEditingCell is intentionally a plain boolean (not useState) — the
  // useNotebookKeyboardShortcuts hook already guards against CodeMirror editors
  // via isContentEditable checks, so no reactive state is needed here.
  const isEditingCell = false
  const [historyCell, setHistoryCell] = useState<string | null>(null)
  const [historyVersions, setHistoryVersions] = useState<CellVersion[]>([])
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showAgent, setShowAgent] = useState(() => {
    try { return localStorage.getItem(`hnb:agentPanel:${id}`) === 'true' } catch { return false }
  })
  const [agentPanelWidth, setAgentPanelWidth] = useState(360)

  // Real-time cell output + metadata updates via WebSocket
  const flashCell = (cellId: string) => {
    let attempts = 0
    const maxAttempts = 20
    const interval = setInterval(() => {
      const el = document.getElementById('cell-' + cellId)
      if (el) {
        clearInterval(interval)
        el.classList.add('cell-flash')
        setTimeout(() => el.classList.remove('cell-flash'), 1500)
      } else if (++attempts >= maxAttempts) {
        clearInterval(interval)
      }
    }, 50)
  }
  useNotebookWs(id, useCallback((cellId: string, outputs: Array<{ type: string; data: unknown }>) => {
    setLocalCells((prev) =>
      prev.map((c) => (c.id === cellId ? { ...c, outputs: outputs as Output[] } : c)),
    )
    setRunningCells((prev) => {
      const next = new Set(prev)
      next.delete(cellId)
      return next
    })
    setCellRunAt((prev) => ({ ...prev, [cellId]: new Date() }))
    flashCell(cellId)
  }, []), useCallback((cellId: string, metadata: Record<string, unknown>) => {
    setLocalCells((prev) =>
      prev.map((c) => (c.id === cellId ? { ...c, metadata } : c)),
    )
    flashCell(cellId)
  }, []))

  const cellsEndRef = useRef<HTMLDivElement>(null)

  // Add-to-dashboard modal
  const [addToDashboardCellId, setAddToDashboardCellId] = useState<string | null>(null)
  const [addToDashboardToast, setAddToDashboardToast] = useState<string | null>(null)

  const { data: notebook, isLoading } = useQuery({
    queryKey: ['notebook', id],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${id}`),
    enabled: !!id,
  })

  const { data: permissions } = useQuery({
    queryKey: ['notebook-permissions', id],
    queryFn: () => api.get<{ can_edit: boolean; can_run: boolean }>(`/api/v1/notebooks/${id}/permissions`),
    enabled: !!id,
  })

  const readOnly = !permissions?.can_edit

  const { data: connectors = [] } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })

  const { data: dashboards = [] } = useQuery({
    queryKey: ['dashboards'],
    queryFn: () => api.get<Dashboard[]>('/api/v1/dashboards'),
    enabled: addToDashboardCellId !== null,
  })

  useEffect(() => {
    localStorage.setItem(paramStorageKey, JSON.stringify(paramValues))
  }, [paramValues, paramStorageKey])

  useEffect(() => {
    localStorage.setItem(`hnb:agentPanel:${id}`, String(showAgent))
  }, [showAgent, id])

  const initializedRef = useRef(false)
  useEffect(() => {
    initializedRef.current = false
  }, [id])
  useEffect(() => {
    if (notebook && !initializedRef.current) {
      initializedRef.current = true
      setLocalCells(notebook.cells)
      setTitleDraft(notebook.title)
      setDescDraft(notebook.description ?? '')
      if (notebook.connector_id) setNotebookConnectorId(notebook.connector_id)
      document.title = `${notebook.title} — Heaven's Notebooks`
    }
    return () => { document.title = "Heaven's Notebooks" }
  }, [notebook])

  const createCell = useMutation({
    mutationFn: ({ type, position }: { type: 'code' | 'text'; position?: number }) =>
      api.post<Cell>(`/api/v1/notebooks/${id}/cells`, {
        type,
        language: type === 'code' ? 'sql' : 'markdown',
        source: '',
        position,
      }),
    onSuccess: (cell) => {
      const withConnector = cell.type === 'code' && notebookConnectorId
        ? { ...cell, connector_id: notebookConnectorId }
        : cell
      if (withConnector.connector_id) {
        assignConnector(cell.id, withConnector.connector_id)
      }
      setLocalCells((prev) => [...prev, withConnector].sort((a, b) => a.position - b.position))
      qc.setQueryData<NotebookWithCells>(['notebook', id], (old) =>
        old ? { ...old, cells: [...(old.cells ?? []), withConnector].sort((a, b) => a.position - b.position) } : old
      )
    },
    onError: (err: Error) => setMutationError(err.message),
  })

  const deleteCell = useMutation({
    mutationFn: (cellId: string) =>
      api.delete(`/api/v1/notebooks/${id}/cells/${cellId}`),
    onSuccess: (_, cellId) => {
      setLocalCells((prev) => prev.filter((c) => c.id !== cellId))
      qc.setQueryData<NotebookWithCells>(['notebook', id], (old) =>
        old ? { ...old, cells: (old.cells ?? []).filter((c) => c.id !== cellId) } : old
      )
    },
    onError: (err: Error) => setMutationError(err.message),
  })

  const duplicateCell = useMutation({
    mutationFn: (cellId: string) =>
      api.post<Cell>(`/api/v1/notebooks/${id}/cells/${cellId}/duplicate`, {}),
    onSuccess: (newCell) => {
      setLocalCells((prev) => {
        const shifted = prev.map((c) =>
          c.position >= newCell.position ? { ...c, position: c.position + 1 } : c
        )
        return [...shifted, newCell].sort((a, b) => a.position - b.position)
      })
      qc.setQueryData<NotebookWithCells>(['notebook', id], (old) =>
        old ? { ...old, cells: [...(old.cells ?? []), newCell] } : old
      )
    },
    onError: (err: Error) => setMutationError(err.message),
  })

  const renameNotebook = useMutation({
    mutationFn: (title: string) =>
      api.put(`/api/v1/notebooks/${id}`, { title }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notebook', id] }),
    onError: (err: Error) => setMutationError(err.message),
  })

  const updateNotebook = useMutation({
    mutationFn: (data: { title?: string; description?: string; connector_id?: string }) =>
      api.put<Notebook>(`/api/v1/notebooks/${id}`, data),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notebook', id] }),
    onError: (err: Error) => setMutationError(err.message),
  })

  const saveParameters = useMutation({
    mutationFn: (params: Parameter[]) =>
      api.put(`/api/v1/notebooks/${id}`, { parameters: params }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notebook', id] }),
    onError: (err: Error) => setMutationError(err.message),
  })

  const switchCellType = useCallback(async (cellId: string) => {
    const cell = localCells.find((c) => c.id === cellId)
    if (!cell) return
    const newType = cell.type === 'code' ? 'text' : 'code'
    const newLanguage = newType === 'code' ? 'sql' : 'markdown'
    await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, {
      type: newType,
      language: newLanguage,
    })
    const updater = (c: Cell): Cell => c.id === cellId ? { ...c, type: newType as Cell['type'], language: newLanguage, outputs: [] } : c
    setLocalCells((prev) => prev.map(updater))
    qc.setQueryData<NotebookWithCells>(['notebook', id], (old) =>
      old ? { ...old, cells: (old.cells ?? []).map(updater) } : old
    )
  }, [id, localCells, qc])

  const updateCellMeta = useCallback(async (cellId: string, updates: Partial<Pick<Cell, 'source_visible' | 'cell_collapsed' | 'slide_break' | 'title' | 'description' | 'slug'>>) => {
    try {
      await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, updates)
      setLocalCells((prev) => prev.map((c) => c.id === cellId ? { ...c, ...updates } : c))
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : 'Failed to update cell')
    }
  }, [id])

  const fetchHistory = useCallback(async (cellId: string) => {
    const versions = await api.get<CellVersion[]>(`/api/v1/notebooks/${id}/cells/${cellId}/versions`)
    setHistoryVersions(versions)
    setHistoryCell(cellId)
  }, [id])

  const restoreVersion = useCallback(async (cellId: string, versionId: string) => {
    try {
      const cell = await api.post<Cell>(`/api/v1/notebooks/${id}/cells/${cellId}/versions/${versionId}/restore`, {})
      setLocalCells((prev) => prev.map((c) => c.id === cell.id ? cell : c))
      setHistoryCell(null)
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : 'Failed to restore version')
    }
  }, [id])

  const saveTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})

  const saveCellSource = useCallback(async (cellId: string, source: string) => {
    setCellSaveState((prev) => ({ ...prev, [cellId]: { saving: true, savedAt: prev[cellId]?.savedAt ?? null, error: null } }))
    try {
      await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { source })
      setCellSaveState((prev) => ({ ...prev, [cellId]: { saving: false, savedAt: new Date(), error: null } }))
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Save failed'
      setCellSaveState((prev) => ({ ...prev, [cellId]: { saving: false, savedAt: prev[cellId]?.savedAt ?? null, error: msg } }))
    }
  }, [id])

  const updateSource = useCallback((cellId: string, source: string) => {
    setLocalCells((prev) => prev.map((c) => (c.id === cellId ? { ...c, source } : c)))
    clearTimeout(saveTimers.current[cellId])
    saveTimers.current[cellId] = setTimeout(() => {
      saveCellSource(cellId, source)
    }, 1500)
  }, [saveCellSource])

  const assignConnector = useCallback(async (cellId: string, connectorId: string) => {
    await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, {
      connector_id: connectorId,
    })
    setLocalCells((prev) =>
      prev.map((c) => (c.id === cellId ? { ...c, connector_id: connectorId } : c))
    )
  }, [id])

  const clearCellConnector = useCallback(async (cellId: string) => {
    await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { connector_id: '' })
    setLocalCells((prev) =>
      prev.map((c) => (c.id === cellId ? { ...c, connector_id: undefined } : c))
    )
  }, [id])

  const updateCellParam = useCallback(async (cellId: string, paramName: string, value: string) => {
    const cell = localCells.find(c => c.id === cellId)
    if (!cell || !cell.parameters) return
    const updated = cell.parameters.map(p => p.name === paramName ? { ...p, default: value } : p)
    setLocalCells(prev => prev.map(c => c.id === cellId ? { ...c, parameters: updated } : c))
    await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { parameters: updated })
  }, [id, localCells])

  const applyNotebookConnector = useCallback((connectorId: string | null) => {
    const val = connectorId ?? ''
    setNotebookConnectorId(val)
    updateNotebook.mutate({ connector_id: val })
  }, [updateNotebook])

  const addCellToDashboard = useCallback(async (dashboardId: string, cellId: string) => {
    const cell = localCells.find(c => c.id === cellId)
    if (!cell) return
    // Fetch existing widgets to compute layout
    const dash = await api.get<Dashboard & { widgets: Widget[] }>(`/api/v1/dashboards/${dashboardId}`)
    const existingWidgets = dash.widgets ?? []
    const maxBottom = existingWidgets.reduce((max: number, w: Widget) => Math.max(max, w.layout.row + w.layout.height), 0)
    const layout = { row: maxBottom, col: 0, width: 6, height: 2 }
    const widgetType = cell.type === 'text' ? 'text' : 'table'
    await api.post(`/api/v1/dashboards/${dashboardId}/widgets`, {
      notebook_id: id,
      cell_id: cellId,
      type: widgetType,
      layout,
      config: {},
    })
    setAddToDashboardCellId(null)
    setAddToDashboardToast(`Added to "${dash.title}"`)
    setTimeout(() => setAddToDashboardToast(null), 3000)
  }, [id, localCells])

  const saveAndRun = useCallback(
    async (cellId: string) => {
      const cell = localCells.find((c) => c.id === cellId)
      if (!cell) return

      // Pre-flight: check connector is assigned
      const effectiveConnectorId = cell.connector_id || notebookConnectorId
      if (!effectiveConnectorId) {
        setLocalCells((prev) =>
          prev.map((c) =>
            c.id === cellId
              ? { ...c, outputs: [{ type: 'error', data: 'No connector selected. Assign a connector to this cell or set a default notebook connector.' }] }
              : c
          )
        )
        return
      }

      await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { source: cell.source })

      setRunningCells((s) => new Set(s).add(cellId))
      try {
        const result = await api.post<{ outputs: Output[] }>(
          `/api/v1/notebooks/${id}/cells/${cellId}/execute`,
          { parameters: paramValues },
        )
        setLocalCells((prev) =>
          prev.map((c) => (c.id === cellId ? { ...c, outputs: result.outputs } : c)),
        )
        setCellRunAt((prev) => ({ ...prev, [cellId]: new Date() }))
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : 'Execution failed'
        setLocalCells((prev) =>
          prev.map((c) =>
            c.id === cellId ? { ...c, outputs: [{ type: 'error', data: msg }] } : c,
          ),
        )
      } finally {
        setRunningCells((s) => {
          const next = new Set(s)
          next.delete(cellId)
          return next
        })
      }
    },
    [id, localCells, paramValues, notebookConnectorId],
  )

  const runAll = useCallback(async () => {
    for (const cell of localCells) {
      if (cell.type === 'code') await saveAndRun(cell.id)
    }
  }, [localCells, saveAndRun])

  useNotebookKeyboardShortcuts(
    {
      runFocusedCell: () => { if (focusedCellId) saveAndRun(focusedCellId) },
      addCellBelow: () => {
        const idx = localCells.findIndex((c) => c.id === focusedCellId)
        const pos = idx >= 0 ? localCells[idx].position + 1 : undefined
        createCell.mutate({ type: 'code', position: pos })
      },
      addCellAbove: () => {
        const idx = localCells.findIndex((c) => c.id === focusedCellId)
        const pos = idx >= 0 ? localCells[idx].position : undefined
        createCell.mutate({ type: 'code', position: pos })
      },
      deleteFocusedCell: () => { if (focusedCellId) deleteCell.mutate(focusedCellId) },
      moveFocusDown: () => {
        if (!focusedCellId) return
        const idx = localCells.findIndex((c) => c.id === focusedCellId)
        if (idx < localCells.length - 1) setFocusedCellId(localCells[idx + 1].id)
      },
      moveFocusUp: () => {
        if (!focusedCellId) return
        const idx = localCells.findIndex((c) => c.id === focusedCellId)
        if (idx > 0) setFocusedCellId(localCells[idx - 1].id)
      },
      convertToMarkdown: () => {
        const cell = localCells.find((c) => c.id === focusedCellId)
        if (focusedCellId && cell?.type !== 'text') switchCellType(focusedCellId)
      },
      convertToCode: () => {
        const cell = localCells.find((c) => c.id === focusedCellId)
        if (focusedCellId && cell?.type !== 'code') switchCellType(focusedCellId)
      },
      openShortcutsModal: () => setShowShortcuts(true),
    },
    isEditingCell
  )

  const moveCell = useCallback((cellId: string, dir: -1 | 1) => {
    setLocalCells((prev) => {
      const idx = prev.findIndex((c) => c.id === cellId)
      if (idx < 0) return prev
      const next = [...prev]
      const swap = idx + dir
      if (swap < 0 || swap >= next.length) return prev
      ;[next[idx], next[swap]] = [next[swap], next[idx]]
      return next
    })
  }, [])

  const runningCount = runningCells.size
  const schemaConnectorId = localCells.find((c) => c.type === 'code' && c.connector_id)?.connector_id ?? (notebookConnectorId || null)

  if (isLoading) return (
    <AppShell noPadding>
      <div style={{ padding: '32px 40px' }}>
        <Skeleton width={120} height={14} style={{ marginBottom: 16 }} />
        <Skeleton width={400} height={32} style={{ marginBottom: 8 }} />
        <Skeleton width={300} height={16} style={{ marginBottom: 32 }} />
        <Skeleton height={120} style={{ marginBottom: 12 }} />
        <Skeleton height={120} style={{ marginBottom: 12 }} />
        <Skeleton height={80} />
      </div>
    </AppShell>
  )
  if (!notebook) return (
    <AppShell noPadding>
      <div style={{ padding: '40px', color: 'var(--text-muted)', textAlign: 'center' }}>
        Notebook not found
      </div>
    </AppShell>
  )

  return (
    <AppShell noPadding>
    <div style={styles.page}>
      {/* Notebook Header */}
      <div style={styles.header}>
        {/* Row 1: breadcrumb + meta */}
        <div style={styles.headerTopRow}>
          <Link to="/" style={styles.backBtn} title="Back to Files">
            <ChevronLeft size={14} style={{ flexShrink: 0 }} />
            <span>Files</span>
          </Link>
          <div style={styles.metaInfo}>
            <span style={styles.metaText}>
              Last updated {fmtTime(new Date(notebook.updated_at))}
            </span>
            {anyCellSaving && (
              <span style={{ ...styles.metaText, color: 'var(--text-muted)', display: 'inline-flex', alignItems: 'center', gap: 3 }}>
                <Loader2 size={10} style={{ animation: 'spin 1s linear infinite' }} />
                Saving…
              </span>
            )}
            {!anyCellSaving && latestCellSave && (
              <span style={{ ...styles.metaText, color: 'var(--success)', display: 'inline-flex', alignItems: 'center', gap: 3 }}>
                <Check size={11} /> All changes saved
              </span>
            )}
            {!anyCellSaving && anyCellError && (
              <span style={{ ...styles.metaText, color: 'var(--error-full)' }}>
                Save error
              </span>
            )}
          </div>
        </div>
        {/* Row 2: title + description */}
        <div style={styles.titleSection}>
          {editingTitle ? (
            <input
              style={styles.titleInput}
              value={titleDraft}
              onChange={(e) => setTitleDraft(e.target.value)}
              onBlur={() => {
                setEditingTitle(false)
                if (titleDraft.trim() && titleDraft.trim() !== notebook.title) {
                  renameNotebook.mutate(titleDraft.trim())
                }
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
                if (e.key === 'Escape') setEditingTitle(false)
              }}
              autoFocus
            />
          ) : (
            <h1
              style={styles.notebookTitle}
              onClick={() => { setTitleDraft(notebook.title); setEditingTitle(true) }}
              title="Click to rename"
            >
              {notebook.title}
            </h1>
          )}
          <input
            style={styles.descInput}
            value={descDraft}
            onChange={(e) => setDescDraft(e.target.value)}
            onBlur={() => {
              if (descDraft !== (notebook?.description ?? '')) {
                updateNotebook.mutate({ description: descDraft })
              }
            }}
            placeholder="Add a description for this notebook…"
          />
        </div>
      </div>

      {/* Toolbar */}
      <div style={styles.toolbar}>
        <div style={styles.toolbarLeft}>
          <ConnectorSelector
            style={styles.connectorSelect}
            value={notebookConnectorId || null}
            onChange={applyNotebookConnector}
            placeholder="Select a connector"
            allowClear
          />
          {runningCount > 0 && (
            <span style={styles.runningBadge}>
              <Loader2 size={12} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 4 }} />
              Running {runningCount} cell{runningCount > 1 ? 's' : ''}…
            </span>
          )}
        </div>
        <div style={styles.toolbarRight}>
          <button
            type="button"
            style={{ ...styles.schemaBtn, ...(showParameters ? styles.schemaBtnActive : {}) }}
            onClick={() => setShowParameters((v) => !v)}
          >
            Parameters
          </button>
          <button
            type="button"
            style={{ ...styles.schemaBtn, ...(showSchema ? styles.schemaBtnActive : {}) }}
            onClick={() => setShowSchema((v) => !v)}
          >
            Schema
          </button>
          <button
            type="button"
            style={{ ...styles.schemaBtn, ...(showSchedules ? styles.schemaBtnActive : {}) }}
            onClick={() => setShowSchedules((v) => !v)}
          >
            Schedules
          </button>
          <button
            type="button"
            style={styles.schemaBtn}
            onClick={() => window.open(`/notebooks/${id}/present`, '_blank')}
          >
            Present
          </button>
          <button type="button" style={styles.runAllBtn} onClick={runAll} disabled={runningCount > 0}>
            <ChevronsRight size={13} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 4 }} />Run All
          </button>
          <button
            type="button"
            style={{ ...styles.schemaBtn, ...(showAgent ? styles.schemaBtnActive : {}), display: 'flex', alignItems: 'center', gap: 4 }}
            onClick={() => setShowAgent((v) => !v)}
          >
            <Bot size={13} /> AI
          </button>
        </div>
      </div>

      {mutationError && (
        <ErrorBanner message={mutationError} onDismiss={() => setMutationError(null)} />
      )}

      {showParameters && (
        <ParametersBar
          parameters={notebook.parameters ?? []}
          values={paramValues}
          onChange={setParamValues}
          onSaveDefinitions={(params) => saveParameters.mutate(params)}
        />
      )}

      {/* Body: optional schema sidebar + cells + optional schedules panel */}
      <div style={styles.body}>
        {showSchema && (
          <SchemaBrowser
            connectorId={schemaConnectorId}
            connector={connectors.find(c => c.id === schemaConnectorId) ?? null}
            onClose={() => setShowSchema(false)}
          />
        )}
        <div style={styles.mainColumn}>
          <div style={styles.cellsArea}>
            <div style={styles.bodyInner}>
              <div style={styles.cells}>
                {localCells.map((cell, i) => (
                  <div key={cell.id}>
                    {cell.type === 'code' && cell.parameters && cell.parameters.length > 0 && (
                      <div style={styles.cellParams}>
                        <span style={styles.cellParamsLabel}>Cell params:</span>
                        {cell.parameters.map((p) => (
                          <div key={p.name} style={styles.cellParam}>
                            <span style={styles.cellParamName}>{p.name}</span>
                            <span style={styles.cellParamEq}>=</span>
                            <input
                              style={styles.cellParamInput}
                              value={p.default}
                              onChange={(e) => updateCellParam(cell.id, p.name, e.target.value)}
                            />
                          </div>
                        ))}
                      </div>
                    )}
                    <NotebookCell
                      cell={cell}
                      connectors={connectors}
                      notebookId={id!}
                      onRun={permissions?.can_run ? saveAndRun : () => {}}
                      onDelete={readOnly ? () => {} : (cid) => {
                        if (confirm('Delete this cell?')) deleteCell.mutate(cid)
                      }}
                      onSourceChange={readOnly ? () => {} : updateSource}
                      onSave={readOnly ? undefined : saveCellSource}
                      onAssignConnector={readOnly ? () => {} : assignConnector}
                      onClearConnector={readOnly ? undefined : clearCellConnector}
                      onMoveUp={readOnly || i === 0 ? undefined : () => moveCell(cell.id, -1)}
                      onMoveDown={readOnly || i === localCells.length - 1 ? undefined : () => moveCell(cell.id, 1)}
                      onSwitchType={readOnly ? undefined : () => switchCellType(cell.id)}
                      onDuplicate={readOnly ? undefined : () => duplicateCell.mutate(cell.id)}
                      running={runningCells.has(cell.id)}
                      saveState={cellSaveState[cell.id]}
                      runAt={cellRunAt[cell.id]}
                      onUpdateCellMeta={readOnly ? undefined : (updates) => updateCellMeta(cell.id, updates)}
                      onShowHistory={readOnly ? undefined : () => fetchHistory(cell.id)}
                      onFocus={(cid) => setFocusedCellId(cid)}
                      onAddToDashboard={readOnly ? undefined : (cid) => setAddToDashboardCellId(cid)}
                      index={i}
                    />
                    <AddCellBar
                      onAddCode={readOnly ? () => {} : () => createCell.mutate({ type: 'code', position: cell.position + 1 })}
                      onAddText={readOnly ? () => {} : () => createCell.mutate({ type: 'text', position: cell.position + 1 })}
                    />
                  </div>
                ))}

                <div style={styles.addRow}>
                  <button type="button" style={styles.addBtn} onClick={readOnly ? () => {} : () => createCell.mutate({ type: 'code' })} disabled={readOnly}>
                    + Code Cell
                  </button>
                  <button type="button" style={styles.addBtn} onClick={readOnly ? () => {} : () => createCell.mutate({ type: 'text' })} disabled={readOnly}>
                    + Text Cell
                  </button>
                </div>
                <div ref={cellsEndRef} />
              </div>
            </div>
          </div>
          {showSchedules && (
            <SchedulesPanel
              notebookId={id!}
              parameters={notebook.parameters ?? []}
            />
          )}
        </div>
        {showAgent && (
          <AgentPanel
            notebookId={id!}
            width={agentPanelWidth}
            onResize={setAgentPanelWidth}
            onClose={() => setShowAgent(false)}
            onCellCreated={() => {
              qc.invalidateQueries({ queryKey: ['notebook', id] })
            }}
            onCellOutput={(cellId, outputs) => {
              setLocalCells((prev) =>
                prev.map((c) => (c.id === cellId ? { ...c, outputs: outputs as Output[] } : c)),
              )
              setRunningCells((prev) => {
                const next = new Set(prev)
                next.delete(cellId)
                return next
              })
              setCellRunAt((prev) => ({ ...prev, [cellId]: new Date() }))
            }}
            onCellScrollTo={(cellId) => {
              let attempts = 0
              const maxAttempts = 50
              const interval = setInterval(() => {
                const el = document.getElementById('cell-' + cellId)
                if (el) {
                  clearInterval(interval)
                  el.scrollIntoView({ behavior: 'smooth', block: 'center' })
                  el.classList.add('cell-flash')
                  setTimeout(() => el.classList.remove('cell-flash'), 3000)
                } else if (++attempts >= maxAttempts) {
                  clearInterval(interval)
                }
              }, 100)
            }}
          />
        )}
      </div>
      {showShortcuts && <ShortcutsModal onClose={() => setShowShortcuts(false)} />}

      {/* Add to dashboard modal */}
      {addToDashboardCellId && (
        <div style={atdStyles.overlay} onClick={() => setAddToDashboardCellId(null)}>
          <div style={atdStyles.modal} onClick={(e) => e.stopPropagation()}>
            <div style={atdStyles.header}>
              <span style={atdStyles.title}>Add to Dashboard</span>
              <button style={atdStyles.close} onClick={() => setAddToDashboardCellId(null)} aria-label="Close">
                <X size={14} />
              </button>
            </div>
            <div style={atdStyles.body}>
              {dashboards.length === 0 ? (
                <p style={atdStyles.empty}>No dashboards found. Create one first.</p>
              ) : (
                <ul style={atdStyles.list}>
                  {dashboards.map((dash) => (
                    <li key={dash.id}>
                      <button
                        style={atdStyles.dashItem}
                        onClick={() => addCellToDashboard(dash.id, addToDashboardCellId)}
                      >
                        {dash.title}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Success toast */}
      {addToDashboardToast && (
        <div style={atdStyles.toast}>
          {addToDashboardToast}
        </div>
      )}

      {historyCell && (
        <>
          <div style={{ position: 'fixed', inset: 0, zIndex: 199 }} onClick={() => setHistoryCell(null)} />
          <div style={{ position: 'fixed', right: 0, top: 0, bottom: 0, width: 300, overflow: 'hidden', display: 'flex', flexDirection: 'column', zIndex: 200 }}>
            <HistoryPanel
              versions={historyVersions}
              currentSource={localCells.find((c) => c.id === historyCell)?.source ?? ''}
              onRestore={(vId) => restoreVersion(historyCell, vId)}
              onClose={() => setHistoryCell(null)}
            />
          </div>
        </>
      )}

    </div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    flex: 1,
    background: 'var(--bg-primary)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    minHeight: 0,
  },

  // ── Header ──
  header: {
    padding: '12px 40px 0',
    display: 'flex',
    flexDirection: 'column' as const,
    gap: 8,
    borderBottom: '1px solid var(--border)',
    background: 'var(--bg-card)',
  },
  headerTopRow: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    width: '100%',
  },
  backBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    color: 'var(--text-muted)',
    textDecoration: 'none',
    fontSize: 13,
    fontWeight: 500,
    flexShrink: 0,
  },
  titleSection: {
    paddingBottom: 16,
  },
  notebookTitle: {
    fontSize: 28,
    fontWeight: 700,
    color: 'var(--text-primary)',
    margin: '0 0 6px',
    cursor: 'pointer',
    lineHeight: 1.2,
  },
  titleInput: {
    fontSize: 28,
    fontWeight: 700,
    color: 'var(--text-primary)',
    background: 'transparent',
    border: 'none',
    borderBottom: '1px solid var(--text-muted)',
    outline: 'none',
    width: '100%',
    fontFamily: 'var(--font-sans)',
    lineHeight: 1.2,
    padding: '2px 0',
    marginBottom: 6,
  },
  descInput: {
    width: '100%',
    border: 'none',
    outline: 'none',
    fontSize: 14,
    color: 'var(--text-muted)',
    background: 'transparent',
    fontFamily: 'var(--font-sans)',
    padding: '1px 0',
  },
  metaInfo: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  metaText: {
    fontSize: 13,
    color: 'var(--text-muted)',
  },

  // ── Toolbar ──
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px 40px',
    borderBottom: '1px solid var(--border-light)',
    flexShrink: 0,
  },
  connectorSelect: {
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    padding: '4px 8px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
    cursor: 'pointer',
    minWidth: 160,
  },
  toolbarLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
  },
  toolbarRight: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
  },
  runningBadge: {
    fontSize: 12,
    color: '#8a8278',
    fontFamily: 'var(--font-mono)',
  },
  runAllBtn: {
    padding: '6px 16px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
  schemaBtn: {
    padding: '5px 12px',
    background: 'none',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    cursor: 'pointer',
  },
  schemaBtnActive: {
    background: 'var(--bg-secondary)',
    border: '1px solid var(--text-muted)',
    color: 'var(--text-primary)',
  },

  // ── Body / cells area ──
  body: {
    flex: 1,
    display: 'flex',
    flexDirection: 'row',
    overflow: 'hidden',
    minHeight: 0,
  },
  mainColumn: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    minWidth: 0,
  },
  cellsArea: {
    flex: 1,
    overflowY: 'auto',
    padding: '32px 0 64px',
  },
  bodyInner: {
    maxWidth: 1200,
    margin: '0 auto',
    padding: '0 40px',
  },

  // Cells: each cell is its own card, separated by gap
  cells: {
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },

  // Cell params bar (above a code cell)
  cellParams: {
    borderBottom: '1px solid var(--border)',
    padding: '4px 16px',
    display: 'flex',
    flexWrap: 'wrap' as const,
    gap: 8,
    alignItems: 'center',
    background: 'var(--bg-secondary)',
  },
  cellParamsLabel: {
    fontSize: 9,
    fontFamily: 'var(--font-mono)',
    fontWeight: 700,
    letterSpacing: '0.1em',
    color: 'var(--text-muted)',
    textTransform: 'uppercase' as const,
  },
  cellParam: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
  },
  cellParamName: {
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    color: 'var(--accent)',
  },
  cellParamEq: {
    color: 'var(--text-muted)',
    fontSize: 11,
  },
  cellParamInput: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 3,
    padding: '1px 5px',
    color: 'var(--text-primary)',
    width: 90,
    outline: 'none',
  },

  // Add cell row
  addRow: {
    display: 'flex',
    gap: 8,
    paddingTop: 12,
  },
  addBtn: {
    padding: '6px 14px',
    border: '1px dashed var(--border)',
    borderRadius: 4,
    background: 'transparent',
    color: 'var(--text-muted)',
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    cursor: 'pointer',
    transition: 'border-color 0.15s, color 0.15s',
  },
}

const atdStyles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'fixed',
    inset: 0,
    background: 'var(--bg-overlay)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 1000,
  },
  modal: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    boxShadow: 'var(--shadow-md)',
    width: 360,
    maxWidth: '90vw',
    maxHeight: '60vh',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '14px 18px',
    borderBottom: '1px solid var(--border)',
  },
  title: {
    fontSize: 14,
    fontWeight: 700,
    color: 'var(--text-primary)',
  },
  close: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    display: 'flex',
    alignItems: 'center',
    padding: '2px 4px',
    borderRadius: 4,
  },
  body: {
    padding: '12px 0',
    overflowY: 'auto',
  },
  empty: {
    padding: '12px 18px',
    fontSize: 13,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
  list: {
    listStyle: 'none',
    margin: 0,
    padding: 0,
  },
  dashItem: {
    display: 'block',
    width: '100%',
    padding: '10px 18px',
    textAlign: 'left',
    background: 'none',
    border: 'none',
    fontSize: 13,
    color: 'var(--text-primary)',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  toast: {
    position: 'fixed',
    bottom: 24,
    left: '50%',
    transform: 'translateX(-50%)',
    background: 'var(--bg-elevated)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: '10px 20px',
    fontSize: 13,
    color: 'var(--text-primary)',
    boxShadow: 'var(--shadow-md)',
    zIndex: 1100,
    whiteSpace: 'nowrap',
  },
}
