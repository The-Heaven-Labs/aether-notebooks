import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api/client'
import type { Notebook } from '../types'
import { NavBar } from '../components/NavBar'

export function HomePage() {
  const qc = useQueryClient()
  const [newTitle, setNewTitle] = useState('')
  const [creating, setCreating] = useState(false)

  const { data: notebooks = [] } = useQuery({
    queryKey: ['notebooks'],
    queryFn: () => api.get<Notebook[]>('/api/v1/notebooks'),
  })

  const createNotebook = useMutation({
    mutationFn: (title: string) =>
      api.post<Notebook>('/api/v1/notebooks', { title }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notebooks'] })
      setNewTitle('')
      setCreating(false)
    },
  })

  const deleteNotebook = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/notebooks/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notebooks'] }),
  })

  return (
    <div style={styles.page}>
      <NavBar activePage="notebooks" />

      <main style={styles.main}>
        <div style={styles.content}>
          <div style={styles.sectionHeader}>
            <div>
              <h2 style={styles.sectionTitle}>Notebooks</h2>
              <p style={styles.sectionSub}>{notebooks.length} notebook{notebooks.length !== 1 ? 's' : ''}</p>
            </div>
            <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>
              + New Notebook
            </button>
          </div>

          {creating && (
            <div style={styles.createForm}>
              <input
                style={styles.createInput}
                value={newTitle}
                onChange={(e) => setNewTitle(e.target.value)}
                placeholder="Notebook title…"
                autoFocus
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && newTitle.trim()) createNotebook.mutate(newTitle.trim())
                  if (e.key === 'Escape') setCreating(false)
                }}
              />
              <button
                type="button"
                style={styles.createBtn}
                disabled={!newTitle.trim()}
                onClick={() => createNotebook.mutate(newTitle.trim())}
              >
                Create
              </button>
              <button type="button" style={styles.cancelBtn} onClick={() => setCreating(false)}>Cancel</button>
            </div>
          )}

          {notebooks.length === 0 && !creating ? (
            <div style={styles.empty}>
              <div style={styles.emptyIcon}>▦</div>
              <p style={styles.emptyTitle}>No notebooks yet</p>
              <p style={styles.emptyText}>Create your first notebook to start querying data.</p>
              <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>
                Create your first notebook
              </button>
            </div>
          ) : (
            <div style={styles.grid}>
              {notebooks.map((nb) => (
                <NotebookCard
                  key={nb.id}
                  notebook={nb}
                  onDelete={() => deleteNotebook.mutate(nb.id)}
                />
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  )
}

function NotebookCard({ notebook, onDelete }: { notebook: Notebook; onDelete: () => void }) {
  const updated = new Date(notebook.updated_at)
  const isToday = new Date().toDateString() === updated.toDateString()
  const dateStr = isToday
    ? `Today at ${updated.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
    : updated.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })

  return (
    <div style={styles.card}>
      <Link to={`/notebooks/${notebook.id}`} style={styles.cardLink}>
        <div style={styles.cardThumb}>
          <span style={styles.cardThumbIcon}>▦</span>
        </div>
        <div style={styles.cardBody}>
          <div style={styles.cardTitle}>{notebook.title}</div>
          <div style={styles.cardMeta}>Updated {dateStr}</div>
        </div>
      </Link>
      <div style={styles.cardFooter}>
        <button
          type="button"
          style={styles.deleteBtn}
          onClick={(e) => { e.preventDefault(); onDelete() }}
        >
          Delete
        </button>
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
  main: {
    flex: 1,
    padding: '40px 32px',
  },
  content: {
    maxWidth: 1280,
    margin: '0 auto',
  },
  sectionHeader: {
    display: 'flex',
    alignItems: 'flex-end',
    justifyContent: 'space-between',
    marginBottom: 24,
  },
  sectionTitle: {
    fontSize: 22,
    fontWeight: 700,
    letterSpacing: '-0.3px',
    color: 'var(--text-primary)',
  },
  sectionSub: {
    fontSize: 13,
    color: 'var(--text-muted)',
    marginTop: 2,
  },
  newBtn: {
    padding: '8px 18px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 7,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    letterSpacing: '0.01em',
  },
  createForm: {
    display: 'flex',
    gap: 10,
    marginBottom: 24,
    padding: 16,
    background: 'white',
    borderRadius: 10,
    border: '1.5px solid var(--accent-light)',
    boxShadow: 'var(--shadow-sm)',
  },
  createInput: {
    flex: 1,
    padding: '8px 12px',
    border: '1.5px solid var(--border)',
    borderRadius: 6,
    fontSize: 14,
    outline: 'none',
    background: 'var(--bg-primary)',
  },
  createBtn: {
    padding: '8px 18px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
  cancelBtn: {
    padding: '8px 16px',
    border: '1px solid var(--border)',
    borderRadius: 6,
    background: 'none',
    fontSize: 13,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
  empty: {
    textAlign: 'center',
    padding: '80px 0',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    gap: 12,
  },
  emptyIcon: {
    width: 56,
    height: 56,
    background: 'var(--bg-secondary)',
    borderRadius: 14,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 28,
    color: 'var(--text-muted)',
    marginBottom: 4,
  },
  emptyTitle: {
    fontSize: 18,
    fontWeight: 700,
    color: 'var(--text-primary)',
    letterSpacing: '-0.2px',
  },
  emptyText: {
    fontSize: 14,
    color: 'var(--text-secondary)',
    marginBottom: 8,
  },
  grid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
    gap: 16,
  },
  card: {
    background: 'white',
    borderRadius: 10,
    border: '1px solid var(--border)',
    overflow: 'hidden',
    boxShadow: 'var(--shadow-sm)',
    display: 'flex',
    flexDirection: 'column',
    transition: 'box-shadow 0.15s, border-color 0.15s',
  },
  cardLink: {
    display: 'flex',
    alignItems: 'center',
    gap: 14,
    padding: '18px 18px 14px',
    textDecoration: 'none',
    flex: 1,
  },
  cardThumb: {
    width: 42,
    height: 42,
    background: 'var(--accent-light)',
    borderRadius: 10,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
  },
  cardThumbIcon: {
    fontSize: 20,
    color: 'var(--accent)',
  },
  cardBody: {
    flex: 1,
    minWidth: 0,
  },
  cardTitle: {
    fontSize: 15,
    fontWeight: 600,
    color: 'var(--text-primary)',
    letterSpacing: '-0.1px',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  cardMeta: {
    fontSize: 12,
    color: 'var(--text-muted)',
    marginTop: 3,
  },
  cardFooter: {
    padding: '10px 18px',
    borderTop: '1px solid var(--border-light)',
    background: 'var(--bg-primary)',
    display: 'flex',
    justifyContent: 'flex-end',
  },
  deleteBtn: {
    padding: '4px 10px',
    border: 'none',
    borderRadius: 5,
    background: 'transparent',
    fontSize: 12,
    fontWeight: 500,
    cursor: 'pointer',
    color: 'var(--error)',
    transition: 'background 0.15s',
  },
}
