import { useState, useEffect, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Notebook, Cell, Output, Connector, Parameter } from '../types'
import { CodeCell } from '../components/CodeCell'
import { TextCell } from '../components/TextCell'
import { ParametersBar } from '../components/ParametersBar'
import { SchemaBrowser } from '../components/SchemaBrowser'
import { SchedulesPanel } from '../components/SchedulesPanel'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

export function NotebookPage() {
  const { id } = useParams<{ id: string }>()
  const qc = useQueryClient()
  const [runningCells, setRunningCells] = useState<Set<string>>(new Set())
  const [localCells, setLocalCells] = useState<Cell[]>([])
  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [paramValues, setParamValues] = useState<Record<string, string>>({})
  const [showSchema, setShowSchema] = useState(false)
  const [showSchedules, setShowSchedules] = useState(false)
  const [mutationError, setMutationError] = useState<string | null>(null)

  const { data: notebook, isLoading } = useQuery({
    queryKey: ['notebook', id],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${id}`),
    enabled: !!id,
  })

  const { data: connectors = [] } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })

  useEffect(() => {
    if (notebook?.cells) setLocalCells(notebook.cells)
  }, [notebook])

  const createCell = useMutation({
    mutationFn: (type: 'code' | 'text') =>
      api.post<Cell>(`/api/v1/notebooks/${id}/cells`, {
        type,
        language: type === 'code' ? 'sql' : 'markdown',
        source: '',
      }),
    onSuccess: (cell) => {
      setLocalCells((prev) => [...prev, cell])
    },
    onError: (err: Error) => setMutationError(err.message),
  })

  const deleteCell = useMutation({
    mutationFn: (cellId: string) =>
      api.delete(`/api/v1/notebooks/${id}/cells/${cellId}`),
    onSuccess: (_, cellId) => {
      setLocalCells((prev) => prev.filter((c) => c.id !== cellId))
    },
    onError: (err: Error) => setMutationError(err.message),
  })

  const renameNotebook = useMutation({
    mutationFn: (title: string) =>
      api.put(`/api/v1/notebooks/${id}`, { title }),
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
    setLocalCells((prev) =>
      prev.map((c) => c.id === cellId ? { ...c, type: newType, language: newLanguage, outputs: [] } : c)
    )
  }, [id, localCells])

  const updateSource = useCallback((cellId: string, source: string) => {
    setLocalCells((prev) => prev.map((c) => (c.id === cellId ? { ...c, source } : c)))
  }, [])

  const assignConnector = useCallback(async (cellId: string, connectorId: string) => {
    await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, {
      connector_id: connectorId,
    })
    setLocalCells((prev) =>
      prev.map((c) => (c.id === cellId ? { ...c, connector_id: connectorId } : c))
    )
  }, [id])

  const saveAndRun = useCallback(
    async (cellId: string) => {
      const cell = localCells.find((c) => c.id === cellId)
      if (!cell) return

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
    [id, localCells, paramValues],
  )

  const runAll = useCallback(async () => {
    for (const cell of localCells) {
      if (cell.type === 'code') await saveAndRun(cell.id)
    }
  }, [localCells, saveAndRun])

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
  const schemaConnectorId = localCells.find((c) => c.type === 'code' && c.connector_id)?.connector_id ?? null

  if (isLoading) return (
    <div style={styles.loadingPage}>
      <div style={styles.loadingDot} />
    </div>
  )
  if (!notebook) return (
    <div style={styles.loadingPage}>
      <p style={{ color: 'var(--text-secondary)' }}>Notebook not found</p>
    </div>
  )

  return (
    <div style={styles.page}>
      {/* Top navigation bar */}
      <header style={styles.header}>
        <div style={styles.headerLeft}>
          <Link to="/" style={styles.backLink}>
            <span style={styles.backArrow}>←</span>
            <span style={styles.backLabel}>Notebooks</span>
          </Link>
          <span style={styles.breadcrumbSep}>/</span>
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
            <span
              style={styles.notebookTitle}
              onClick={() => { setTitleDraft(notebook.title); setEditingTitle(true) }}
              title="Click to rename"
            >
              {notebook.title}
            </span>
          )}
        </div>
        <div style={styles.headerRight}>
          {runningCount > 0 && (
            <span style={styles.runningBadge}>
              ⏳ Running {runningCount} cell{runningCount > 1 ? 's' : ''}…
            </span>
          )}
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
          <button type="button" style={styles.runAllBtn} onClick={runAll} disabled={runningCount > 0}>
            ▶▶ Run All
          </button>
        </div>
      </header>

      {mutationError && (
        <div style={{ background: '#fff0f0', borderBottom: '1px solid #fcd0d0', padding: '6px 24px', fontSize: 12, color: '#c0392b', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          {mutationError}
          <button type="button" onClick={() => setMutationError(null)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#c0392b', fontSize: 14, padding: 0 }}>✕</button>
        </div>
      )}

      <ParametersBar
        parameters={notebook.parameters ?? []}
        values={paramValues}
        onChange={setParamValues}
        onSaveDefinitions={(params) => saveParameters.mutate(params)}
      />

      {/* Body: optional schema sidebar + cells + optional schedules panel */}
      <div style={styles.body}>
        {showSchema && (
          <SchemaBrowser
            connectorId={schemaConnectorId}
            onClose={() => setShowSchema(false)}
          />
        )}
        <div style={styles.mainColumn}>
          <div style={styles.cellsArea}>
            <div style={styles.bodyInner}>
              <div style={styles.cells}>
                {localCells.map((cell, i) =>
                  cell.type === 'code' ? (
                    <CodeCell
                      key={cell.id}
                      cell={cell}
                      connectors={connectors}
                      onRun={saveAndRun}
                      onDelete={(cid) => deleteCell.mutate(cid)}
                      onSourceChange={updateSource}
                      onMoveUp={i > 0 ? () => moveCell(cell.id, -1) : undefined}
                      onMoveDown={i < localCells.length - 1 ? () => moveCell(cell.id, 1) : undefined}
                      onSwitchType={() => switchCellType(cell.id)}
                      onAssignConnector={assignConnector}
                      running={runningCells.has(cell.id)}
                    />
                  ) : (
                    <TextCell
                      key={cell.id}
                      cell={cell}
                      onDelete={(cid) => deleteCell.mutate(cid)}
                      onSourceChange={updateSource}
                      onMoveUp={i > 0 ? () => moveCell(cell.id, -1) : undefined}
                      onMoveDown={i < localCells.length - 1 ? () => moveCell(cell.id, 1) : undefined}
                      onSwitchType={() => switchCellType(cell.id)}
                    />
                  ),
                )}

                <div style={styles.addRow}>
                  <button type="button" style={styles.addBtn} onClick={() => createCell.mutate('code')}>
                    + Code Cell
                  </button>
                  <button type="button" style={styles.addBtn} onClick={() => createCell.mutate('text')}>
                    + Text Cell
                  </button>
                </div>
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
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: '100vh',
    background: 'var(--bg-primary)',
    display: 'flex',
    flexDirection: 'column',
  },
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
  header: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    height: 52,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 24px',
    flexShrink: 0,
    position: 'sticky',
    top: 0,
    zIndex: 100,
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
    transition: 'color 0.15s',
  },
  backArrow: {
    fontSize: 16,
    lineHeight: 1,
  },
  backLabel: {
    fontSize: 13,
  },
  breadcrumbSep: {
    color: '#3a3630',
    fontSize: 14,
    flexShrink: 0,
  },
  notebookTitle: {
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
  runningBadge: {
    fontSize: 12,
    color: '#8a8278',
    fontFamily: 'var(--font-mono)',
  },
  runAllBtn: {
    padding: '6px 16px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    letterSpacing: '0.01em',
  },
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
  schemaBtn: {
    padding: '6px 14px',
    background: 'transparent',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    fontSize: 13,
    fontWeight: 500,
    cursor: 'pointer',
    letterSpacing: '0.01em',
  },
  schemaBtnActive: {
    background: 'var(--bg-secondary)',
    borderColor: 'var(--accent)',
    color: 'var(--accent)',
  },
  cells: {
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
  addRow: {
    display: 'flex',
    gap: 10,
    paddingTop: 8,
  },
  addBtn: {
    padding: '8px 20px',
    border: '1.5px dashed var(--border)',
    borderRadius: 7,
    background: 'transparent',
    color: 'var(--text-muted)',
    fontSize: 13,
    fontWeight: 500,
    cursor: 'pointer',
    transition: 'border-color 0.15s, color 0.15s',
  },
}
