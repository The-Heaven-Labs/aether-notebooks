import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { ChevronsRight, ChevronLeft, Loader2, X, Check, GripVertical, Shield, Clock, Trash2, Globe } from 'lucide-react'
import { DndContext, closestCenter, PointerSensor, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, verticalListSortingStrategy, useSortable, arrayMove } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { AppShell } from '../components/AppShell'
import { Skeleton } from '../components/Skeleton'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Notebook, Cell, Output, Connector, Parameter, CellVersion, NotebookSnapshot, Dashboard, Widget } from '../types'
import type { ChartConfig } from '../charts'
import { Cell as NotebookCell, focusCellEditorEnd, collabCache, updateCellScroll, type NotebookCollab } from '../components/Cell'
import { focusMarkdownCell } from '../utils/editorFocus'
import { ParametersBar } from '../components/ParametersBar'
import { SchemaBrowser } from '../components/SchemaBrowser'
import { SchedulesPanel } from '../components/SchedulesPanel'
import { useNotebookKeyboardShortcuts } from '../hooks/useNotebookKeyboardShortcuts'
import { HistoryPanel } from '../components/HistoryPanel'
import { NotebookHistoryPanel } from '../components/NotebookHistoryPanel'
import { ConnectorSelector } from '../components/ConnectorSelector'
import { ErrorBanner } from '../components/ErrorBanner'
import { CollaboratorAvatars } from '../components/CollaboratorAvatars'
import { useNotebookWs } from '../hooks/useNotebookWs'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { PermissionsPanel } from '../components/PermissionsPanel'
import { ShareModal } from '../components/ShareModal'
import { exportNotebookHTML } from '../utils/notebookExport'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
  can_share?: boolean
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

// Stable noop function to avoid creating new references each render
const noop = () => {}

function SortableCellWrapper({ children, id }: { children: React.ReactNode; id: string }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id })
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    position: 'relative',
  }
  return (
    <div ref={setNodeRef} style={style} {...attributes}>
      <div
        {...listeners}
        style={{
          position: 'absolute',
          left: -28,
          top: 12,
          cursor: 'grab',
          color: 'var(--text-muted)',
          opacity: 0.4,
          display: 'flex',
          zIndex: 5,
        }}
        title="Drag to reorder"
      >
        <GripVertical size={16} />
      </div>
      {children}
    </div>
  )
}

export function NotebookPage() {
  const { id } = useParams<{ id: string }>()
  const qc = useQueryClient()
  const [collabVersion, setCollabVersion] = useState(0)
  const [runningCells, setRunningCells] = useState<Set<string>>(new Set())
  const [localCells, setLocalCells] = useState<Cell[]>([])
  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [descDraft, setDescDraft] = useState('')
  const [editingDesc, setEditingDesc] = useState(false)
  const paramStorageKey = `aether_params_${id}`
  const [paramValues, setParamValues] = useState<Record<string, string>>(() => {
    try { return JSON.parse(localStorage.getItem(paramStorageKey) ?? '{}') } catch { return {} }
  })
  const [showSchema, setShowSchema] = useState(false)
  const [showSchedules, setShowSchedules] = useState(false)
  const [showParameters, setShowParameters] = useState(false)
  const [notebookConnectorId, setNotebookConnectorId] = useState<string>('')
  const [mutationError, setMutationError] = useState<string | null>(null)
  const [showPermissions, setShowPermissions] = useState(false)
  const [showShare, setShowShare] = useState(false)
  const [embedCellId, setEmbedCellId] = useState<string | undefined>(undefined)
  const [showHistory, setShowHistory] = useState(false)
  const [historySnapshots, setHistorySnapshots] = useState<NotebookSnapshot[]>([])
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

  const backUrl = (() => { try { const f = sessionStorage.getItem('aether_last_folder'); return f ? `/?folder=${f}` : '/' } catch { return '/' } })()

  const anyCellError = useMemo(() =>
    Object.values(cellSaveState).some(s => s.error),
    [cellSaveState]
  )

  const localCellsRef = useRef(localCells)
  localCellsRef.current = localCells
  const paramValuesRef = useRef(paramValues)
  paramValuesRef.current = paramValues
  const notebookConnectorIdRef = useRef(notebookConnectorId)
  notebookConnectorIdRef.current = notebookConnectorId

  const [following, setFollowing] = useState<{ email: string; name: string } | null>(null)
  const [viewOpen, setViewOpen] = useState(false)
  const [shareOpen, setShareOpen] = useState(false)
  const [cellRunAt, setCellRunAt] = useState<Record<string, Date>>({})
  const [focusedCellId, setFocusedCellId] = useState<string | null>(null)
  const [allCollapsed, setAllCollapsed] = useState(false)
  const [allCodeHidden, setAllCodeHidden] = useState(false)
  const [allOutputsHidden, setAllOutputsHidden] = useState(false)
  const [, setTick] = useState(0) // forces re-render to update "X minutes ago" text

  // Refresh the "Last updated" relative time every 30 seconds
  useEffect(() => {
    const id = setInterval(() => setTick(t => t + 1), 30_000)
    return () => clearInterval(id)
  }, [])
  const [isEditingCell, setIsEditingCell] = useState(false)
  const autoFocusCellRef = useRef(false)
  const pendingExecRef = useRef(new Set<string>())
  const [historyCell, setHistoryCell] = useState<string | null>(null)
  const [historyVersions, setHistoryVersions] = useState<CellVersion[]>([])
  // Drag-and-drop sensors
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event
    if (!over || active.id === over.id) return
    setLocalCells((prev) => {
      const oldIndex = prev.findIndex(c => c.id === active.id)
      const newIndex = prev.findIndex(c => c.id === over.id)
      if (oldIndex < 0 || newIndex < 0) return prev
      return arrayMove(prev, oldIndex, newIndex)
    })
  }, [])

  // Real-time cell output + metadata updates via WebSocket
  const flashCell = (cellId: string) => {
    let attempts = 0
    const maxAttempts = 20
    const interval = setInterval(() => {
      const el = document.getElementById('cell-' + cellId)
      if (el) {
        clearInterval(interval)
        el.scrollIntoView({ behavior: 'smooth', block: 'center' })
        el.classList.add('cell-flash')
        setTimeout(() => el.classList.remove('cell-flash'), 1500)
      } else if (++attempts >= maxAttempts) {
        clearInterval(interval)
      }
    }, 50)
  }
  const shouldScroll = useCallback((userEmail?: string) => {
    return following && userEmail === following.email
  }, [following])

  useNotebookWs(id, useCallback((cellId: string, outputs: Array<{ type: string; data: unknown }>, userEmail?: string, totalTimeMs?: number) => {
    setLocalCells((prev) => {
      const metrics = totalTimeMs ? { connect_time_ms: 0, query_time_ms: 0, render_time_ms: 0, total_time_ms: totalTimeMs } : undefined
      return prev.map((c) => (c.id === cellId ? { ...c, outputs: outputs as Output[], metrics: metrics || c.metrics } : c))
    })
    setRunningCells((prev) => {
      const next = new Set(prev)
      next.delete(cellId)
      return next
    })
    setCellRunAt((prev) => ({ ...prev, [cellId]: new Date() }))
    if (!pendingExecRef.current.has(cellId) && shouldScroll(userEmail)) {
      flashCell(cellId)
    }
  }, [shouldScroll]), useCallback((cellId: string, metadata: Record<string, unknown>, userEmail?: string) => {
    setLocalCells((prev) =>
      prev.map((c) => (c.id === cellId ? { ...c, metadata } : c)),
    )
    if (!pendingExecRef.current.has(cellId) && (userEmail === 'agent@aether' || shouldScroll(userEmail))) {
      flashCell(cellId)
    }
  }, [shouldScroll]), useCallback((cellId: string, updates: Record<string, unknown>, userEmail?: string) => {
    // cell_updated event received — apply broadcast fields to local cache
    // Skip source for regular users (Yjs is source of truth), but apply
    // it for agent updates since Yjs may not be synced in real-time.
    const isAgent = userEmail === 'agent@aether'
    const { source: _source, ...rest } = updates as Record<string, unknown> & { source?: unknown }
    const payload = isAgent ? (updates as Record<string, unknown>) : rest
    if (Object.keys(payload).length > 0) {
      setLocalCells((prev) =>
        prev.map((c) => c.id === cellId ? { ...c, ...payload } as Cell : c),
      )
      qc.setQueryData<NotebookWithCells>(['notebook', id], (old) =>
        old ? { ...old, cells: old.cells.map((c) => c.id === cellId ? { ...c, ...payload } as Cell : c) } : old,
      )
    }
    if (!pendingExecRef.current.has(cellId) && shouldScroll(userEmail)) {
      flashCell(cellId)
    }
  }, [id, qc, shouldScroll]), useCallback((cell: Cell, userEmail?: string) => {
    setLocalCells((prev) => {
      if (prev.some((c) => c.id === cell.id)) return prev
      const shifted = prev.map((c) =>
        c.position >= cell.position ? { ...c, position: c.position + 1 } : c
      )
      return [...shifted, cell].sort((a, b) => a.position - b.position)
    })
    qc.setQueryData<NotebookWithCells>(['notebook', id], (old) => {
      if (!old || old.cells.some((c) => c.id === cell.id)) return old
      const shifted = old.cells.map((c) =>
        c.position >= cell.position ? { ...c, position: c.position + 1 } : c
      )
      return { ...old, cells: [...shifted, cell].sort((a, b) => a.position - b.position) }
    })
    if (!pendingExecRef.current.has(cell.id) && shouldScroll(userEmail)) {
      flashCell(cell.id)
    }
  }, [id, qc, shouldScroll]), useCallback((cellId: string) => {
    setLocalCells((prev) => prev.filter((c) => c.id !== cellId))
    qc.setQueryData<NotebookWithCells>(['notebook', id], (old) =>
      old ? { ...old, cells: old.cells.filter((c) => c.id !== cellId) } : old,
    )
  }, [id, qc]), useCallback(() => {
    qc.invalidateQueries({ queryKey: ['notebook', id] })
  }, [id, qc]), useCallback((cellId: string) => {
    setRunningCells((prev) => new Set(prev).add(cellId))
  }, []), useCallback((data: { running_cells?: string[] }) => {
    const cells = data.running_cells
    if (cells?.length) {
      setRunningCells((prev) => {
        const next = new Set(prev)
        for (const id of cells) next.add(id)
        return next
      })
    }
  }, []))

  // Scroll to cell from URL hash (#cell-{id})
  useEffect(() => {
    if (!id) return
    const hash = window.location.hash
    if (hash.startsWith('#cell-')) {
      const cellId = hash.slice(6)
      const timer = setInterval(() => {
        const el = document.getElementById('cell-' + cellId)
        if (el) {
          clearInterval(timer)
          el.scrollIntoView({ behavior: 'smooth', block: 'center' })
          el.classList.add('cell-flash')
          setTimeout(() => el.classList.remove('cell-flash'), 1500)
        }
      }, 100)
      return () => clearInterval(timer)
    }
  }, [id])

  // Awareness: follow the followed user's focus
  useEffect(() => {
    const c = id ? collabCache.get(id) : undefined
    const awareness = c?.provider?.awareness
    if (!awareness) return

    const handler = () => {
      if (!following) return
      const states = awareness.getStates()
      for (const [, state] of states) {
        if (state.user?.email === following.email && state.focus) {
          if (state.focus.scrollTop != null && cellsContainerRef.current) {
            cellsContainerRef.current.scrollTop = state.focus.scrollTop
          } else if (state.focus.cellId) {
            const el = document.getElementById('cell-' + state.focus.cellId)
            if (el) {
              el.scrollIntoView({ behavior: 'smooth', block: 'center' })
            }
          }
          break
        }
      }
    }

    handler()
    awareness.on('change', handler)
    return () => awareness.off('change', handler)
  }, [id, following, collabVersion])

  // Poll awareness states when following (belt-and-suspenders for change events)
  useEffect(() => {
    if (!following || !id) return
    const c = collabCache.get(id)
    const awareness = c?.provider?.awareness
    if (!awareness) return

    const check = () => {
      if (!following) return
      const states = awareness.getStates()
      for (const [, state] of states) {
        if (state.user?.email === following.email && state.focus) {
          if (state.focus.scrollTop != null && cellsContainerRef.current) {
            cellsContainerRef.current.scrollTop = state.focus.scrollTop
          } else if (state.focus.cellId) {
            const el = document.getElementById('cell-' + state.focus.cellId)
            if (el) {
              el.scrollIntoView({ behavior: 'smooth', block: 'center' })
            }
          }
          break
        }
      }
    }

    check()
    const timer = setInterval(check, 800)
    return () => clearInterval(timer)
  }, [id, following, collabVersion])

  // Awareness: broadcast who we're following
  useEffect(() => {
    if (!id) return
    const collab = collabCache.get(id)
    if (!collab?.provider.awareness) return
    collab.provider.awareness.setLocalStateField('following', {
      email: following?.email ?? null,
    })
  }, [id, following, collabVersion])

  // Escape key unfollow
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && following) {
        setFollowing(null)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [following])

  const cellsContainerRef = useRef<HTMLDivElement>(null)

  // Throttled scroll tracking + persist to sessionStorage
  useEffect(() => {
    const el = cellsContainerRef.current
    if (!el || !id) return
    let ticking = false
    const handler = () => {
      if (!ticking) {
        requestAnimationFrame(() => {
          updateCellScroll(id, el.scrollTop)
          try { sessionStorage.setItem(`aether_scroll_${id}`, String(el.scrollTop)) } catch {}
          ticking = false
        })
        ticking = true
      }
    }
    el.addEventListener('scroll', handler, { passive: true })
    return () => el.removeEventListener('scroll', handler)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, localCells.length])

  // Restore scroll position on mount (after cells have loaded)
  useEffect(() => {
    if (!id || localCells.length === 0) return
    const el = cellsContainerRef.current
    if (!el) return
    const saved = (() => { try { return sessionStorage.getItem(`aether_scroll_${id}`) } catch { return null } })()
    if (!saved) return
    const scrollTop = parseInt(saved, 10)
    if (isNaN(scrollTop)) return
    // Smooth animated scroll to saved position, then re-apply to handle lazy loading
    const start = el.scrollTop
    const duration = 700
    setTimeout(() => {
      const startTime = performance.now()
      const animate = (now: number) => {
        const t = Math.min((now - startTime) / duration, 1)
        const ease = 1 - Math.pow(1 - t, 3)
        el.scrollTop = start + (scrollTop - start) * ease
        if (t < 1) requestAnimationFrame(animate)
        else {
          // Re-apply for ~250ms after animation completes to handle lazy content
          let attempts = 8
          const correct = () => {
            if (!el || attempts-- <= 0) return
            if (Math.abs(el.scrollTop - scrollTop) > 5) el.scrollTop = scrollTop
            requestAnimationFrame(correct)
          }
          requestAnimationFrame(correct)
        }
      }
      requestAnimationFrame(animate)
    }, 150)
  }, [id, localCells.length])

  const cellsEndRef = useRef<HTMLDivElement>(null)

  // Add-to-dashboard modal
  const [addToDashboardCellId, setAddToDashboardCellId] = useState<string | null>(null)
  const [addToDashboardToast, setAddToDashboardToast] = useState<string | null>(null)
  const [deleteCellTarget, setDeleteCellTarget] = useState<string | null>(null)
  const [deleteNotebookConfirm, setDeleteNotebookConfirm] = useState(false)
  const navigate = useNavigate()

  const { data: notebook, isLoading, error: notebookError } = useQuery({
    queryKey: ['notebook', id],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${id}`),
    enabled: !!id,
  })

  const readOnly = !notebook?.can_edit

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

  const initializedRef = useRef(false)
  useEffect(() => {
    initializedRef.current = false
  }, [id])
  useEffect(() => {
    if (notebook && !initializedRef.current) {
      initializedRef.current = true
      setTitleDraft(notebook.title)
      setDescDraft(notebook.description ?? '')
      if (notebook.connector_id) setNotebookConnectorId(notebook.connector_id)
      document.title = `${notebook.title} — Aether Notebooks`
    }
    // Always sync localCells with notebook data (for agent updates)
    if (notebook) {
      setLocalCells(prev => {
        let changed = false
        const merged = notebook.cells.map(nbCell => {
          const local = prev.find(c => c.id === nbCell.id)
          if (local && local.source === nbCell.source) {
            return local
          }
          changed = true
          const cell = local ? { ...nbCell, source: local.source, outputs: local.outputs } : { ...nbCell }
          // Derive display metrics from persisted duration_ms
          if (cell.duration_ms != null && !cell.metrics) {
            cell.metrics = { connect_time_ms: 0, query_time_ms: 0, render_time_ms: 0, total_time_ms: cell.duration_ms }
          }
          return cell
        })
        return changed ? merged : prev
      })
    }
    return () => { document.title = "Aether Notebooks" }
  }, [notebook])

  // Auto-focus first cell when notebook loads
  useEffect(() => {
    if (notebook && notebook.cells.length > 0 && !focusedCellId) {
      setFocusedCellId(notebook.cells[0].id)
    }
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
      setLocalCells((prev) => {
        if (prev.some((c) => c.id === withConnector.id)) return prev
        const shifted = prev.map((c) =>
          c.position >= withConnector.position ? { ...c, position: c.position + 1 } : c
        )
        return [...shifted, withConnector].sort((a, b) => a.position - b.position)
      })
      qc.setQueryData<NotebookWithCells>(['notebook', id], (old) => {
        if (!old || old.cells.some((c) => c.id === withConnector.id)) return old
        const shifted = old.cells.map((c) =>
          c.position >= withConnector.position ? { ...c, position: c.position + 1 } : c
        )
        return { ...old, cells: [...shifted, withConnector].sort((a, b) => a.position - b.position) }
      })
      setTimeout(() => {
        const el = document.getElementById('cell-' + cell.id)
        if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      }, 100)
      if (autoFocusCellRef.current) {
        autoFocusCellRef.current = false
        setFocusedCellId(cell.id)
      }
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

  const deleteNotebookMut = useMutation({
    mutationFn: () => api.delete(`/api/v1/notebooks/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notebooks'] })
      navigate('/')
    },
    onError: (err: Error) => setMutationError(err.message),
  })

  const switchCellType = useCallback(async (cellId: string) => {
    setIsEditingCell(false)
    const cells = localCellsRef.current
    const cell = cells.find((c) => c.id === cellId)
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
  }, [id, qc])

  const updateCellMeta = useCallback(async (cellId: string, updates: Partial<Pick<Cell, 'source_visible' | 'outputs_hidden' | 'cell_collapsed' | 'slide_break' | 'title' | 'slug'>>) => {
    try {
      await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, updates)
      setLocalCells((prev) => prev.map((c) => c.id === cellId ? { ...c, ...updates } : c))
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : 'Failed to update cell')
    }
  }, [id])

  const updateCellChartConfig = useCallback(async (cellId: string, config: ChartConfig) => {
    try {
      const cells = localCellsRef.current
      setLocalCells((prev) => prev.map((c) => c.id === cellId ? { ...c, metadata: { ...c.metadata, chart: config } } : c))
      const cell = cells.find((c) => c.id === cellId)
      const metadata = { ...cell?.metadata, chart: config }
      await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { metadata })
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : 'Failed to update chart config')
    }
  }, [id])

  const updateCellViewMode = useCallback(async (cellId: string, viewMode: 'table' | 'chart') => {
    try {
      const cells = localCellsRef.current
      setLocalCells((prev) => prev.map((c) => c.id === cellId ? { ...c, metadata: { ...c.metadata, viewMode } } : c))
      const cell = cells.find((c) => c.id === cellId)
      const metadata = { ...cell?.metadata, viewMode }
      await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { metadata })
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : 'Failed to update view mode')
    }
  }, [id])

  const fetchHistory = useCallback(async (cellId: string) => {
    const versions = await api.get<CellVersion[]>(`/api/v1/notebooks/${id}/cells/${cellId}/versions`)
    setHistoryVersions(versions)
    setHistoryCell(cellId)
  }, [id])

  // Stable handlers for Cell memoization (must be after all referenced functions)
  const stableFocusHandler = useCallback((cid: string) => setFocusedCellId(cid), [])
  const stableDeleteHandler = useCallback((cid: string) => {
    setDeleteCellTarget(cid)
  }, [])
  const stableDashboardHandler = useCallback((cid: string) => setAddToDashboardCellId(cid), [])
  const stableHistoryHandler = useCallback((cid: string) => fetchHistory(cid), [fetchHistory])
  const stableEmbedHandler = useCallback((cid: string) => { setEmbedCellId(cid); setShowShare(true) }, [])
  const stableOnEditStart = useCallback(() => setIsEditingCell(true), [])
  const stableOnEditEnd = useCallback(() => setIsEditingCell(false), [])
  const stableDuplicate = useCallback((cid: string) => duplicateCell.mutate(cid), [duplicateCell])
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

  const stableMoveUp = useCallback((cid: string) => moveCell(cid, -1), [moveCell])
  const stableMoveDown = useCallback((cid: string) => moveCell(cid, 1), [moveCell])

  const restoreVersion = useCallback(async (cellId: string, versionId: string) => {
    try {
      const cell = await api.post<Cell>(`/api/v1/notebooks/${id}/cells/${cellId}/versions/${versionId}/restore`, {})
      setLocalCells((prev) => prev.map((c) => c.id === cell.id ? cell : c))
      setHistoryCell(null)
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : 'Failed to restore version')
    }
  }, [id])

  const openSnapshotHistory = useCallback(async () => {
    try {
      const snaps = await api.get<NotebookSnapshot[]>(`/api/v1/notebooks/${id}/snapshots`)
      setHistorySnapshots(snaps)
      setShowHistory(true)
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : 'Failed to load version history')
    }
  }, [id])

  const createSnapshot = useCallback(async (name: string) => {
    await api.post(`/api/v1/notebooks/${id}/snapshots`, { name })
    // Refresh the list
    const snaps = await api.get<NotebookSnapshot[]>(`/api/v1/notebooks/${id}/snapshots`)
    setHistorySnapshots(snaps)
  }, [id])

  const restoreSnapshot = useCallback(async (snapshotId: string) => {
    await api.post(`/api/v1/notebooks/${id}/snapshots/${snapshotId}/restore`, {})
    setShowHistory(false)
    // Reload the notebook data
    qc.invalidateQueries({ queryKey: ['notebook', id] })
  }, [id, qc])

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
    setLocalCells((prev) => {
      const cell = prev.find(c => c.id === cellId)
      // If source is the same as what we already have, skip (no save needed)
      if (cell && cell.source === source) return prev
      return prev.map((c) => (c.id === cellId ? { ...c, source } : c))
    })

    // Check if agent just updated this cell — suppress auto-save
    const cells = localCellsRef.current
    const cell = cells.find(c => c.id === cellId)
    if (cell?.agent_updated_at) {
      const elapsed = Date.now() - new Date(cell.agent_updated_at).getTime()
      if (elapsed < 5000) {
        // Agent update is recent (< 5s), don't trigger auto-save
        // The agent already updated Yjs, no need to save again
        return
      }
    }

    // Normal auto-save flow
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
    const cells = localCellsRef.current
    const cell = cells.find(c => c.id === cellId)
    if (!cell || !cell.parameters) return
    const updated = cell.parameters.map(p => p.name === paramName ? { ...p, default: value } : p)
    setLocalCells(prev => prev.map(c => c.id === cellId ? { ...c, parameters: updated } : c))
    await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { parameters: updated })
  }, [id])

  const applyNotebookConnector = useCallback((connectorId: string | null) => {
    const val = connectorId ?? ''
    setNotebookConnectorId(val)
    updateNotebook.mutate({ connector_id: val })
  }, [updateNotebook])

  const addCellToDashboard = useCallback(async (dashboardId: string, cellId: string) => {
    const cells = localCellsRef.current
    const cell = cells.find(c => c.id === cellId)
    if (!cell) return
    // Fetch existing widgets to compute layout
    const dash = await api.get<Dashboard & { widgets: Widget[] }>(`/api/v1/dashboards/${dashboardId}`)
    const existingWidgets = dash.widgets ?? []
    const maxBottom = existingWidgets.reduce((max: number, w: Widget) => Math.max(max, w.layout.row + w.layout.height), 0)
    const layout = { row: maxBottom, col: 0, width: 6, height: 8 }
    const chartMeta = (cell as any).metadata?.chart
    const hasChart = !!chartMeta?.chartType
    const widgetType = cell.type === 'text' ? 'text' : hasChart ? 'chart' : 'table'
    await api.post(`/api/v1/dashboards/${dashboardId}/widgets`, {
      notebook_id: id,
      cell_id: cellId,
      type: widgetType,
      layout,
      config: chartMeta ?? {},
    })
    setAddToDashboardCellId(null)
    setAddToDashboardToast(`Added to "${dash.title}"`)
    setTimeout(() => setAddToDashboardToast(null), 3000)
  }, [id])

  const saveAndRun = useCallback(
    async (cellId: string) => {
      const cells = localCellsRef.current
      const params = paramValuesRef.current
      const nbConnectorId = notebookConnectorIdRef.current
      const cell = cells.find((c) => c.id === cellId)
      if (!cell) return

      // Pre-flight: check connector is assigned
      const effectiveConnectorId = cell.connector_id || nbConnectorId
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
      pendingExecRef.current.add(cellId)
      try {
        const result = await api.post<{ outputs: Output[]; metrics?: { connect_time_ms: number; query_time_ms: number; render_time_ms: number; total_time_ms: number } }>(
          `/api/v1/notebooks/${id}/cells/${cellId}/execute`,
          { parameters: params },
        )
        setLocalCells((prev) =>
          prev.map((c) => (c.id === cellId ? { ...c, outputs: result.outputs, metrics: result.metrics } : c)),
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
        setTimeout(() => pendingExecRef.current.delete(cellId), 3000)
        setRunningCells((s) => {
          const next = new Set(s)
          next.delete(cellId)
          return next
        })
      }
    },
    [id],
  )

  const runAll = useCallback(async () => {
    const cells = localCellsRef.current
    for (const cell of cells) {
      if (cell.type === 'code') await saveAndRun(cell.id)
    }
  }, [saveAndRun])

  const toggleCollapseAll = useCallback(async () => {
    const newCollapsed = !allCollapsed
    setAllCollapsed(newCollapsed)
    for (const cell of localCells) {
      await api.put(`/api/v1/notebooks/${id}/cells/${cell.id}`, {
        source_visible: !newCollapsed,
        cell_collapsed: newCollapsed,
      })
    }
    setLocalCells(prev => prev.map(c => ({
      ...c,
      source_visible: !newCollapsed,
      cell_collapsed: newCollapsed,
    })))
  }, [allCollapsed, localCells, id])

  const toggleAllCode = useCallback(async () => {
    const newHidden = !allCodeHidden
    setAllCodeHidden(newHidden)
    for (const cell of localCells) {
      if (cell.type === 'code') {
        await api.put(`/api/v1/notebooks/${id}/cells/${cell.id}`, {
          source_visible: !newHidden,
        })
      }
    }
    setLocalCells(prev => prev.map(c =>
      c.type === 'code' ? { ...c, source_visible: !newHidden } : c
    ))
  }, [allCodeHidden, localCells, id])

  const toggleAllOutputs = useCallback(async () => {
    const newHidden = !allOutputsHidden
    setAllOutputsHidden(newHidden)
    for (const cell of localCells) {
      if (cell.type === 'code') {
        await api.put(`/api/v1/notebooks/${id}/cells/${cell.id}`, {
          outputs_hidden: newHidden,
        })
      }
    }
    setLocalCells(prev => prev.map(c =>
      c.type === 'code' ? { ...c, outputs_hidden: newHidden } : c
    ))
  }, [allOutputsHidden, localCells, id])

  useNotebookKeyboardShortcuts(
    {
      runFocusedCell: () => { if (focusedCellId) saveAndRun(focusedCellId) },
      runFocusedCellAndAdvance: () => {
        if (!focusedCellId) return
        const cellId = focusedCellId
        const idx = localCells.findIndex((c) => c.id === cellId)
        if (idx < localCells.length - 1) {
          const nextId = localCells[idx + 1].id
          setFocusedCellId(nextId)
          saveAndRun(cellId).then(() => requestAnimationFrame(() => {
            const el = document.getElementById('cell-' + nextId)
            if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
          }))
        } else {
          saveAndRun(cellId)
          createCell.mutate({ type: 'code', position: localCells[idx].position + 1 })
        }
      },
      addCellBelow: () => {
        const idx = localCells.findIndex((c) => c.id === focusedCellId)
        const pos = idx >= 0 ? localCells[idx].position + 1 : undefined
        autoFocusCellRef.current = true
        createCell.mutate({ type: 'code', position: pos })
      },
      addCellAbove: () => {
        const idx = localCells.findIndex((c) => c.id === focusedCellId)
        const pos = idx >= 0 ? localCells[idx].position : undefined
        autoFocusCellRef.current = true
        createCell.mutate({ type: 'code', position: pos })
      },
      deleteFocusedCell: () => {
        if (!focusedCellId) return
        const idx = localCells.findIndex((c) => c.id === focusedCellId)
        if (idx >= 0) {
          if (idx < localCells.length - 1) {
            setFocusedCellId(localCells[idx + 1].id)
          } else if (idx > 0) {
            setFocusedCellId(localCells[idx - 1].id)
          }
        }
        deleteCell.mutate(focusedCellId)
      },
      moveFocusDown: () => {
        if (!focusedCellId) return
        const idx = localCells.findIndex((c) => c.id === focusedCellId)
        if (idx < localCells.length - 1) {
          const nextId = localCells[idx + 1].id
          setFocusedCellId(nextId)
          requestAnimationFrame(() => {
            const el = document.getElementById('cell-' + nextId)
            if (el) el.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
          })
        }
      },
      moveFocusUp: () => {
        if (!focusedCellId) return
        const idx = localCells.findIndex((c) => c.id === focusedCellId)
        if (idx > 0) {
          const prevId = localCells[idx - 1].id
          setFocusedCellId(prevId)
          requestAnimationFrame(() => {
            const el = document.getElementById('cell-' + prevId)
            if (el) el.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
          })
        }
      },
      moveCellUp: () => { if (focusedCellId) moveCell(focusedCellId, -1) },
      moveCellDown: () => { if (focusedCellId) moveCell(focusedCellId, 1) },
      convertToMarkdown: () => {
        const cell = localCells.find((c) => c.id === focusedCellId)
        if (focusedCellId && cell?.type !== 'text') switchCellType(focusedCellId)
      },
      convertToCode: () => {
        const cell = localCells.find((c) => c.id === focusedCellId)
        if (focusedCellId && cell?.type !== 'code') switchCellType(focusedCellId)
      },
      enterEditMode: () => {
        if (!focusedCellId) return
        setIsEditingCell(true)
        requestAnimationFrame(() => {
          focusCellEditorEnd(focusedCellId)
          focusMarkdownCell(focusedCellId)
        })
      },
      exitEditMode: () => {
        setIsEditingCell(false)
        // Blur the active element to exit CodeMirror
        const active = document.activeElement as HTMLElement | null
        if (active?.closest('.cm-editor')) active.blur()
      },
      duplicateCell: () => { if (focusedCellId) duplicateCell.mutate(focusedCellId) },
      toggleSlideBreak: () => {
        const cell = localCells.find((c) => c.id === focusedCellId)
        if (focusedCellId && cell) updateCellMeta(focusedCellId, { slide_break: !cell.slide_break })
      },
    },
    isEditingCell
  )

  const runningCount = runningCells.size
  const schemaConnectorId = notebookConnectorId || null
  const userEmail = localStorage.getItem('aether_user_email') ?? ''
  const [collab, setCollab] = useState<NotebookCollab | null>(null)
  useEffect(() => {
    if (!id) { setCollab(null); return }
    const initial = collabCache.get(id)
    setCollab(initial ?? null)
    if (initial) setCollabVersion(v => v + 1)
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail
      if (detail.notebookId === id) { setCollab(collabCache.get(id) ?? null); setCollabVersion(v => v + 1) }
    }
    window.addEventListener('aether-collab', handler)
    return () => window.removeEventListener('aether-collab', handler)
  }, [id])

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
        {notebookError ? String(notebookError) : 'Notebook not found'}
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
          <Link to={backUrl} style={styles.backBtn} title="Back to Files">
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
        {/* Owner info */}
        {notebook.owner_name && (
          <div style={styles.ownerRow}>
            <span style={styles.ownerText}>
              Created by {notebook.owner_name}{notebook.owner_email ? ` (${notebook.owner_email})` : ''}
            </span>
          </div>
        )}
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
          {editingDesc ? (
            <input
              style={styles.descInput}
              value={descDraft}
              onChange={(e) => setDescDraft(e.target.value)}
              onBlur={() => {
                setEditingDesc(false)
                if (descDraft !== (notebook?.description ?? '')) {
                  updateNotebook.mutate({ description: descDraft })
                }
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === 'Escape') (e.target as HTMLInputElement).blur()
              }}
              autoFocus
            />
          ) : (
            <div
              style={styles.descRendered}
              onClick={() => { setDescDraft(notebook.description ?? ''); setEditingDesc(true) }}
              title="Click to edit description"
            >
              {notebook.description ? (
                <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}>
                  {notebook.description}
                </ReactMarkdown>
              ) : (
                <span style={styles.descPlaceholder}>Add a description for this notebook…</span>
              )}
            </div>
          )}
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
          <CollaboratorAvatars
            provider={collab?.provider}
            currentUserEmail={userEmail}
            following={following}
            onFollow={(c) => setFollowing({ email: c.email, name: c.name })}
            onUnfollow={() => setFollowing(null)}
            showAgent={true}
            onFollowAgent={() => {
              if (following?.email === 'agent@aether') {
                setFollowing(null)
              } else {
                setFollowing({ email: 'agent@aether', name: 'AI Agent' })
              }
            }}
          />
        </div>
        <div style={styles.toolbarRight}>
          {/* View dropdown */}
          <div style={{ position: 'relative' }}>
            <button
              type="button"
              style={{ ...styles.schemaBtn, ...(viewOpen ? styles.schemaBtnActive : {}) }}
              onClick={() => { setViewOpen(v => !v); setShareOpen(false) }}
            >
              View ▾
            </button>
            {viewOpen && (
              <>
                <div style={styles.dropdownBackdrop} onClick={() => setViewOpen(false)} />
                <div style={styles.dropdown}>
                  <button
                    type="button"
                    style={{ ...styles.dropdownItem, ...(showParameters ? styles.dropdownItemActive : {}) }}
                    onClick={() => { setShowParameters(v => !v); setViewOpen(false) }}
                  >
                    Parameters
                  </button>
                  <button
                    type="button"
                    style={{ ...styles.dropdownItem, ...(showSchema ? styles.dropdownItemActive : {}) }}
                    onClick={() => { setShowSchema(v => !v); setViewOpen(false) }}
                  >
                    Schema
                  </button>
                  <button
                    type="button"
                    style={{ ...styles.dropdownItem, ...(showSchedules ? styles.dropdownItemActive : {}) }}
                    onClick={() => { setShowSchedules(v => !v); setViewOpen(false) }}
                  >
                    Schedules
                  </button>
                  <div style={styles.dropdownSeparator} />
                  <button
                    type="button"
                    style={styles.dropdownItem}
                    onClick={() => { toggleCollapseAll(); setViewOpen(false) }}
                  >
                    {allCollapsed ? 'Show All' : 'Collapse All'}
                  </button>
                  <button
                    type="button"
                    style={styles.dropdownItem}
                    onClick={() => { toggleAllCode(); setViewOpen(false) }}
                  >
                    {allCodeHidden ? 'Show Code' : 'Hide Code'}
                  </button>
                  <button
                    type="button"
                    style={styles.dropdownItem}
                    onClick={() => { toggleAllOutputs(); setViewOpen(false) }}
                  >
                    {allOutputsHidden ? 'Show Outputs' : 'Hide Outputs'}
                  </button>
                </div>
              </>
            )}
          </div>

          {/* Run All — standalone */}
          <button type="button" style={styles.runAllBtn} onClick={runAll} disabled={runningCount > 0}>
            <ChevronsRight size={13} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 4 }} />Run All
          </button>

          {/* History — standalone */}
          <button
            type="button"
            style={{ ...styles.schemaBtn, ...(showHistory ? styles.schemaBtnActive : {}), display: 'flex', alignItems: 'center', gap: 4 }}
            onClick={openSnapshotHistory}
          >
            <Clock size={13} /> History
          </button>

          {/* Delete notebook */}
          {notebook?.can_edit && (
            <button
              type="button"
              style={{ ...styles.schemaBtn, color: 'var(--text-muted)' }}
              onClick={() => setDeleteNotebookConfirm(true)}
              title="Delete notebook"
            >
              <Trash2 size={13} /> Delete
            </button>
          )}

          {/* Share dropdown */}
          <div style={{ position: 'relative' }}>
            <button
              type="button"
              style={{ ...styles.schemaBtn, ...(shareOpen ? styles.schemaBtnActive : {}) }}
              onClick={() => { setShareOpen(v => !v); setViewOpen(false) }}
            >
              Share ▾
            </button>
            {shareOpen && (
              <>
                <div style={styles.dropdownBackdrop} onClick={() => setShareOpen(false)} />
                <div style={styles.dropdown}>
                  <button
                    type="button"
                    style={styles.dropdownItem}
                    onClick={() => {
                      const token = localStorage.getItem('aether_token')
                      fetch(`/api/v1/notebooks/${id}/export`, { headers: { Authorization: `Bearer ${token}` } })
                        .then(r => r.blob())
                        .then(blob => {
                          const url = URL.createObjectURL(blob)
                          const a = document.createElement('a')
                          a.href = url
                          a.download = `${notebook.title}.ipynb`
                          document.body.appendChild(a)
                          a.click()
                          document.body.removeChild(a)
                          URL.revokeObjectURL(url)
                          setShareOpen(false)
                        })
                    }}
                  >
                    Export (.ipynb)
                  </button>
                  <button
                    type="button"
                    style={styles.dropdownItem}
                    onClick={() => {
                      exportNotebookHTML({ ...notebook, cells: localCellsRef.current })
                      setShareOpen(false)
                    }}
                  >
                    Export (HTML)
                  </button>
                  <button
                    type="button"
                    style={styles.dropdownItem}
                    onClick={() => { window.open(`/notebooks/${id}/present`, '_blank'); setShareOpen(false) }}
                  >
                    Present mode
                  </button>
                  {notebook?.can_edit && (
                    <>
                      <div style={styles.dropdownSeparator} />
                      <button
                        type="button"
                        style={styles.dropdownItem}
                        onClick={() => { setShowPermissions(true); setShareOpen(false) }}
                      >
                        <Shield size={13} style={{ marginRight: 6 }} /> Permissions
                      </button>
                    </>
                  )}
                  {notebook?.can_share !== false && (
                    <>
                      <div style={styles.dropdownSeparator} />
                      <button
                        type="button"
                        style={styles.dropdownItem}
                        onClick={() => { setShowShare(true); setShareOpen(false) }}
                      >
                        <Globe size={13} style={{ marginRight: 6 }} /> Public link
                      </button>
                    </>
                  )}

                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {mutationError && (
        <ErrorBanner message={mutationError} onDismiss={() => setMutationError(null)} />
      )}

      {(showParameters || (notebook?.parameters?.length ?? 0) > 0) && (
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
            onClose={() => setShowSchema(false)}
          />
        )}
        <div style={styles.mainColumn}>
          <div ref={cellsContainerRef} style={styles.cellsArea}>
            <div style={styles.bodyInner}>
              <div style={styles.cells}>
                {!readOnly && (
                  <AddCellBar
                    onAddCode={() => {
                      const first = localCells[0]
                      createCell.mutate({ type: 'code', position: first ? first.position : 0 })
                    }}
                    onAddText={() => {
                      const first = localCells[0]
                      createCell.mutate({ type: 'text', position: first ? first.position : 0 })
                    }}
                  />
                )}
                <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
                  <SortableContext items={localCells.map(c => c.id)} strategy={verticalListSortingStrategy}>
                    {localCells.map((cell, i) => (
                      <SortableCellWrapper key={cell.id} id={cell.id}>
                        <div data-cell-id={cell.id}>
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
                            onRun={notebook?.can_run ? saveAndRun : noop}
                            onDelete={readOnly ? noop : stableDeleteHandler}
                            onSourceChange={readOnly ? noop : updateSource}
                            onSave={readOnly ? undefined : saveCellSource}
                            onAssignConnector={readOnly ? noop as any : assignConnector}
                            onClearConnector={readOnly ? undefined : clearCellConnector}
                            onMoveUp={readOnly || i === 0 ? undefined : stableMoveUp}
                            onMoveDown={readOnly || i === localCells.length - 1 ? undefined : stableMoveDown}
                            onSwitchType={readOnly ? undefined : switchCellType}
                            onDuplicate={readOnly ? undefined : stableDuplicate}
                            running={runningCells.has(cell.id)}
                            saveState={cellSaveState[cell.id]}
                            runAt={cellRunAt[cell.id]}
                            metrics={cell.metrics}
                            onUpdateCellMeta={readOnly ? undefined : updateCellMeta}
                            onChartConfigChange={readOnly ? undefined : updateCellChartConfig}
                            onViewModeChange={readOnly ? undefined : updateCellViewMode}
                            onShowHistory={readOnly ? undefined : stableHistoryHandler}
                            onFocus={stableFocusHandler}
                            onEditStart={stableOnEditStart}
                            onEditEnd={stableOnEditEnd}
                             onAddToDashboard={readOnly ? undefined : stableDashboardHandler}
                             onEmbed={readOnly ? undefined : stableEmbedHandler}
                             canShare={notebook?.can_share !== false}
                             focused={cell.id === focusedCellId}
                            index={i}
                            paramValues={(() => {
                              const merged = { ...paramValues }
                              if (notebook?.parameters) {
                                for (const p of notebook.parameters) {
                                  if (!(p.name in merged)) merged[p.name] = p.default
                                }
                              }
                              return merged
                            })()}
                          />
                          {!readOnly && (
                            <AddCellBar
                              onAddCode={() => createCell.mutate({ type: 'code', position: cell.position + 1 })}
                              onAddText={() => createCell.mutate({ type: 'text', position: cell.position + 1 })}
                            />
                          )}
                        </div>
                      </SortableCellWrapper>
                    ))}
                  </SortableContext>
                </DndContext>

                {!readOnly && (
                  <div style={styles.addRow}>
                    <button type="button" style={styles.addBtn} onClick={() => createCell.mutate({ type: 'code' })}>
                      + Code Cell
                    </button>
                    <button type="button" style={styles.addBtn} onClick={() => createCell.mutate({ type: 'text' })}>
                      + Text Cell
                    </button>
                  </div>
                )}
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

      </div>

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

      {showHistory && (
        <>
          <div style={{ position: 'fixed', inset: 0, zIndex: 199 }} onClick={() => setShowHistory(false)} />
          <div style={{ position: 'fixed', right: 0, top: 0, bottom: 0, width: 380, overflow: 'hidden', display: 'flex', flexDirection: 'column', zIndex: 200 }}>
            <NotebookHistoryPanel
              snapshots={historySnapshots}
              onCreateSnapshot={createSnapshot}
              onRestore={restoreSnapshot}
              onClose={() => setShowHistory(false)}
              canEdit={notebook?.can_edit ?? false}
            />
          </div>
        </>
      )}

    </div>
    <ConfirmDialog
      open={!!deleteCellTarget}
      title="Delete cell"
      message="Delete this cell? This cannot be undone."
      confirmLabel="Delete"
      destructive
      onConfirm={() => { if (deleteCellTarget) deleteCell.mutate(deleteCellTarget); setDeleteCellTarget(null) }}
      onCancel={() => setDeleteCellTarget(null)}
    />
    <ConfirmDialog
      open={deleteNotebookConfirm}
      title="Delete notebook"
      message={`Delete "${notebook?.title ?? 'this notebook'}"? It will be moved to trash and automatically deleted after 7 days.`}
      confirmLabel="Delete notebook"
      destructive
      onConfirm={() => { setDeleteNotebookConfirm(false); deleteNotebookMut.mutate() }}
      onCancel={() => setDeleteNotebookConfirm(false)}
    />
    {showPermissions && notebook && (
      <PermissionsPanel
        resourceType="notebook"
        resourceId={notebook.id}
        resourceName={notebook.title}
        parentFolderId={notebook.folder_id}
        resourceOwnerId={notebook.created_by}
        canEdit={notebook.can_edit}
        onClose={() => setShowPermissions(false)}
      />
    )}
    {showShare && notebook && (
      <ShareModal
        resourceType="notebook"
        resourceId={notebook.id}
        canShare={notebook.can_share ?? false}
        onClose={() => { setShowShare(false); setEmbedCellId(undefined) }}
        initialTab={embedCellId ? 'embed' : undefined}
        initialCellId={embedCellId}
      />
    )}
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
  descRendered: {
    fontSize: 14,
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-sans)',
    cursor: 'pointer',
    lineHeight: 1.6,
    padding: '2px 0',
    minHeight: 24,
  },
  descPlaceholder: {
    color: 'var(--text-muted)',
    opacity: 0.6,
    fontStyle: 'italic',
  },
  ownerRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  ownerText: {
    fontSize: 12,
    color: 'var(--text-muted)',
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
  dropdown: {
    position: 'absolute' as const,
    top: '100%',
    right: 0,
    marginTop: 4,
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
    zIndex: 100,
    minWidth: 180,
    padding: '4px 0',
  },
  dropdownBackdrop: {
    position: 'fixed' as const,
    inset: 0,
    zIndex: 99,
  },
  dropdownItem: {
    display: 'flex',
    alignItems: 'center',
    width: '100%',
    padding: '6px 12px',
    border: 'none',
    background: 'none',
    cursor: 'pointer',
    fontSize: 13,
    color: 'var(--text-primary)',
    textAlign: 'left' as const,
  },
  dropdownItemActive: {
    display: 'flex',
    alignItems: 'center',
    width: '100%',
    padding: '6px 12px',
    border: 'none',
    background: 'var(--bg-secondary)',
    cursor: 'pointer',
    fontSize: 13,
    color: 'var(--accent)',
    textAlign: 'left' as const,
  },
  dropdownSeparator: {
    height: 1,
    background: 'var(--border-light)',
    margin: '4px 0',
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
    padding: '32px 0 80px',
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
