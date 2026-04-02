import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams } from 'react-router-dom'
import { ChevronsRight, X, Loader2 } from 'lucide-react'
import { AppShell } from '../components/AppShell'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Notebook, Cell, Output, Connector, Parameter, CellVersion } from '../types'
import { CodeCell } from '../components/CodeCell'
import { TextCell } from '../components/TextCell'
import { ParametersBar } from '../components/ParametersBar'
import { SchemaBrowser } from '../components/SchemaBrowser'
import { SchedulesPanel } from '../components/SchedulesPanel'
import { useNotebookKeyboardShortcuts } from '../hooks/useNotebookKeyboardShortcuts'
import { HistoryPanel } from '../components/HistoryPanel'
import { ShortcutsModal } from '../components/ShortcutsModal'
import { ConnectorSelector } from '../components/ConnectorSelector'

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
  const [cellRunAt, setCellRunAt] = useState<Record<string, Date>>({})
  const [focusedCellId, setFocusedCellId] = useState<string | null>(null)
  // isEditingCell is intentionally a plain boolean (not useState) — the
  // useNotebookKeyboardShortcuts hook already guards against CodeMirror editors
  // via isContentEditable checks, so no reactive state is needed here.
  const isEditingCell = false
  const [historyCell, setHistoryCell] = useState<string | null>(null)
  const [historyVersions, setHistoryVersions] = useState<CellVersion[]>([])
  const [showShortcuts, setShowShortcuts] = useState(false)

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
    localStorage.setItem(paramStorageKey, JSON.stringify(paramValues))
  }, [paramValues, paramStorageKey])

  useEffect(() => {
    if (notebook) {
      setLocalCells(notebook.cells)
      setTitleDraft(notebook.title)
      setDescDraft(notebook.description ?? '')
      // Init notebook-level connector from persisted value
      if (notebook.connector_id) setNotebookConnectorId(notebook.connector_id)
      document.title = `${notebook.title} — Heaven's Notebooks`
    }
    return () => { document.title = "Heaven's Notebooks" }
  }, [notebook])

  const createCell = useMutation({
    mutationFn: (type: 'code' | 'text') =>
      api.post<Cell>(`/api/v1/notebooks/${id}/cells`, {
        type,
        language: type === 'code' ? 'sql' : 'markdown',
        source: '',
      }),
    onSuccess: (cell) => {
      const withConnector = cell.type === 'code' && notebookConnectorId
        ? { ...cell, connector_id: notebookConnectorId }
        : cell
      if (withConnector.connector_id) {
        assignConnector(cell.id, withConnector.connector_id)
      }
      setLocalCells((prev) => [...prev, withConnector])
      qc.setQueryData<NotebookWithCells>(['notebook', id], (old) =>
        old ? { ...old, cells: [...(old.cells ?? []), withConnector] } : old
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

  const updateCellMeta = useCallback(async (cellId: string, updates: Partial<Pick<Cell, 'source_visible' | 'cell_collapsed' | 'title' | 'description' | 'slug'>>) => {
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
    [id, localCells, paramValues],
  )

  const runAll = useCallback(async () => {
    for (const cell of localCells) {
      if (cell.type === 'code') await saveAndRun(cell.id)
    }
  }, [localCells, saveAndRun])

  useNotebookKeyboardShortcuts(
    {
      runFocusedCell: () => { if (focusedCellId) saveAndRun(focusedCellId) },
      addCellBelow: () => createCell.mutate('code'),
      addCellAbove: () => createCell.mutate('code'),
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
  const schemaConnectorId = localCells.find((c) => c.type === 'code' && c.connector_id)?.connector_id ?? null

  if (isLoading) return (
    <AppShell noPadding>
      <div style={styles.loadingPage}><div style={styles.loadingDot} /></div>
    </AppShell>
  )
  if (!notebook) return (
    <AppShell noPadding>
      <div style={styles.loadingPage}><p style={{ color: 'var(--text-secondary)' }}>Notebook not found</p></div>
    </AppShell>
  )

  return (
    <AppShell noPadding>
    <div style={styles.page}>
      {/* Slim notebook toolbar */}
      <div style={styles.toolbar}>
        <div style={styles.toolbarLeft}>
          <ConnectorSelector
            style={styles.connectorSelect}
            value={notebookConnectorId || null}
            onChange={applyNotebookConnector}
            placeholder="Connection…"
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
            <ChevronsRight size={13} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 4 }} /> Run All
          </button>
        </div>
      </div>

      {mutationError && (
        <div style={{ background: 'var(--error-light)', borderBottom: '1px solid var(--error-border)', padding: '6px 24px', fontSize: 12, color: 'var(--error-full)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          {mutationError}
          <button type="button" onClick={() => setMutationError(null)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--error-full)', fontSize: 14, padding: 0, display: 'flex', alignItems: 'center' }}><X size={14} /></button>
        </div>
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
              <div style={styles.notebookHeading}>
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
                  placeholder="Add a description…"
                />
              </div>

              <div style={styles.cells}>
                {localCells.map((cell, i) =>
                  cell.type === 'code' ? (
                    <div key={cell.id}>
                      {cell.parameters && cell.parameters.length > 0 && (
                        <div style={{
                          borderBottom: '1px solid var(--border)',
                          padding: '4px 12px',
                          display: 'flex',
                          flexWrap: 'wrap',
                          gap: 6,
                          alignItems: 'center',
                          background: 'var(--bg-secondary)',
                          fontSize: 11,
                        }}>
                          <span style={{ color: 'var(--text-muted)', fontWeight: 600, letterSpacing: '0.05em', textTransform: 'uppercase', fontSize: 10 }}>Cell params:</span>
                          {cell.parameters.map(p => (
                            <div key={p.name} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                              <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--accent)', fontSize: 11 }}>{p.name}</span>
                              <span style={{ color: 'var(--text-muted)' }}>=</span>
                              <input
                                style={{
fontSize: 11,
                                   fontFamily: 'var(--font-mono)',
                                   background: 'var(--bg-primary)',
                                   border: '1px solid var(--border)',
                                   borderRadius: 4,
                                   padding: '1px 5px',
                                  color: 'var(--text-primary)',
                                  width: 90,
                                  outline: 'none',
                                }}
                                value={p.default}
                                onChange={e => updateCellParam(cell.id, p.name, e.target.value)}
                              />
                            </div>
                          ))}
                        </div>
                      )}
                      <CodeCell
                        cell={cell}
                        connectors={connectors}
                        notebookId={id!}
                        onRun={saveAndRun}
                        onDelete={(cid) => deleteCell.mutate(cid)}
                        onSourceChange={updateSource}
                        onMoveUp={i > 0 ? () => moveCell(cell.id, -1) : undefined}
                        onMoveDown={i < localCells.length - 1 ? () => moveCell(cell.id, 1) : undefined}
                        onSwitchType={() => switchCellType(cell.id)}
                        onAssignConnector={assignConnector}
                        onClearConnector={clearCellConnector}
                        running={runningCells.has(cell.id)}
                        saveState={cellSaveState[cell.id]}
                        runAt={cellRunAt[cell.id]}
                        onUpdateCellMeta={(updates) => updateCellMeta(cell.id, updates)}
                        onShowHistory={() => fetchHistory(cell.id)}
                        onFocus={(cid) => setFocusedCellId(cid)}
                      />
                    </div>
                  ) : (
                    <TextCell
                      key={cell.id}
                      cell={cell}
                      onDelete={(cid) => deleteCell.mutate(cid)}
                      onSourceChange={updateSource}
                      onSave={saveCellSource}
                      onMoveUp={i > 0 ? () => moveCell(cell.id, -1) : undefined}
                      onMoveDown={i < localCells.length - 1 ? () => moveCell(cell.id, 1) : undefined}
                      onSwitchType={() => switchCellType(cell.id)}
                      saveState={cellSaveState[cell.id]}
                      onUpdateCellMeta={(updates) => updateCellMeta(cell.id, updates)}
                      onShowHistory={() => fetchHistory(cell.id)}
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
      {showShortcuts && <ShortcutsModal onClose={() => setShowShortcuts(false)} />}
      {historyCell && (
        <div style={{ position: 'fixed', right: 0, top: 0, bottom: 0, zIndex: 200 }}>
          <HistoryPanel
            versions={historyVersions}
            currentSource={localCells.find((c) => c.id === historyCell)?.source ?? ''}
            onRestore={(vId) => restoreVersion(historyCell, vId)}
            onClose={() => setHistoryCell(null)}
          />
        </div>
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
  },
  loadingPage: {
    flex: 1,
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
    borderRadius: 6,
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
    cursor: 'pointer',
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
  notebookHeading: {
    marginBottom: 32,
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
    borderBottom: '2px solid var(--accent)',
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
    fontSize: 15,
    color: 'var(--text-muted)',
    background: 'transparent',
    fontFamily: 'var(--font-sans)',
    padding: '1px 0',
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
    borderRadius: 6,
    background: 'transparent',
    color: 'var(--text-muted)',
    fontSize: 13,
    fontWeight: 500,
    cursor: 'pointer',
    transition: 'border-color 0.15s, color 0.15s',
  },
}
