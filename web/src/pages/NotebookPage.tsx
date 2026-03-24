import { useState, useEffect, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { Notebook, Cell, Output } from '../types'
import { CodeCell } from '../components/CodeCell'
import { TextCell } from '../components/TextCell'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

export function NotebookPage() {
  const { id } = useParams<{ id: string }>()
  const qc = useQueryClient()
  const [runningCells, setRunningCells] = useState<Set<string>>(new Set())
  const [localCells, setLocalCells] = useState<Cell[]>([])

  const { data: notebook, isLoading } = useQuery({
    queryKey: ['notebook', id],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${id}`),
    enabled: !!id,
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
  })

  const deleteCell = useMutation({
    mutationFn: (cellId: string) =>
      api.delete(`/api/v1/notebooks/${id}/cells/${cellId}`),
    onSuccess: (_, cellId) => {
      setLocalCells((prev) => prev.filter((c) => c.id !== cellId))
    },
  })

  const updateSource = useCallback((cellId: string, source: string) => {
    setLocalCells((prev) => prev.map((c) => (c.id === cellId ? { ...c, source } : c)))
  }, [])

  const saveAndRun = useCallback(
    async (cellId: string) => {
      const cell = localCells.find((c) => c.id === cellId)
      if (!cell) return

      // Save source
      await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { source: cell.source })

      setRunningCells((s) => new Set(s).add(cellId))
      try {
        const result = await api.post<{ outputs: Output[] }>(
          `/api/v1/notebooks/${id}/cells/${cellId}/execute`,
          {},
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
    [id, localCells],
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

  if (isLoading) return <div style={styles.loading}>Loading…</div>
  if (!notebook) return <div style={styles.loading}>Notebook not found</div>

  return (
    <div style={styles.page}>
      <div style={styles.header}>
        <h1 style={styles.title}>{notebook.title}</h1>
        <div style={styles.actions}>
          <button style={styles.btn} onClick={runAll}>▶▶ Run All</button>
        </div>
      </div>

      <div style={styles.cells}>
        {localCells.map((cell, i) =>
          cell.type === 'code' ? (
            <CodeCell
              key={cell.id}
              cell={cell}
              onRun={saveAndRun}
              onDelete={(cid) => deleteCell.mutate(cid)}
              onSourceChange={updateSource}
              onMoveUp={i > 0 ? () => moveCell(cell.id, -1) : undefined}
              onMoveDown={i < localCells.length - 1 ? () => moveCell(cell.id, 1) : undefined}
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
            />
          ),
        )}
      </div>

      <div style={styles.addButtons}>
        <button style={styles.addBtn} onClick={() => createCell.mutate('code')}>
          + Code Cell
        </button>
        <button style={styles.addBtn} onClick={() => createCell.mutate('text')}>
          + Text Cell
        </button>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: { maxWidth: 900, margin: '0 auto', padding: '32px 24px' },
  loading: { padding: 40, textAlign: 'center', color: 'var(--text-secondary)' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 },
  title: { fontSize: 22, fontWeight: 700 },
  actions: { display: 'flex', gap: 8 },
  btn: {
    padding: '6px 14px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 13,
    cursor: 'pointer',
  },
  cells: { display: 'flex', flexDirection: 'column', gap: 16 },
  addButtons: { display: 'flex', gap: 12, marginTop: 20 },
  addBtn: {
    padding: '8px 18px',
    border: '1px dashed var(--border)',
    borderRadius: 6,
    background: 'transparent',
    color: 'var(--text-secondary)',
    fontSize: 13,
    cursor: 'pointer',
  },
}
