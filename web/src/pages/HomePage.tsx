import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { api } from '../api/client'
import type { Notebook } from '../types'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { LayoutGrid, List, BookOpen } from 'lucide-react'

export function HomePage() {
  useEffect(() => { document.title = "Notebooks — Heaven's Notebooks" }, [])
  const qc = useQueryClient()
  const [newTitle, setNewTitle] = useState('')
  const [creating, setCreating] = useState(false)
  const [layout, setLayout] = useState<'grid' | 'list'>(() =>
    (localStorage.getItem('hnb_notebooks_layout') as 'grid' | 'list') ?? 'list'
  )
  const toggleLayout = () => {
    const next = layout === 'list' ? 'grid' : 'list'
    setLayout(next)
    localStorage.setItem('hnb_notebooks_layout', next)
  }

  const { data: notebooks = [] } = useQuery({
    queryKey: ['notebooks'],
    queryFn: () => api.get<Notebook[]>('/api/v1/notebooks'),
  })

  const [createError, setCreateError] = useState<string | null>(null)

  const createNotebook = useMutation({
    mutationFn: (title: string) =>
      api.post<Notebook>('/api/v1/notebooks', { title }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notebooks'] })
      setNewTitle('')
      setCreating(false)
      setCreateError(null)
    },
    onError: (err: Error) => setCreateError(err.message),
  })

  const deleteNotebook = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/notebooks/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notebooks'] }),
    onError: (err: Error) => setCreateError(err.message),
  })

  return (
    <AppShell>
        <div style={styles.content}>
          <div style={styles.sectionHeader}>
            <div>
              <h2 style={styles.sectionTitle}>Notebooks</h2>
              <p style={styles.sectionSub}>{notebooks.length} notebook{notebooks.length !== 1 ? 's' : ''}</p>
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              <button type="button" style={styles.layoutBtn} onClick={toggleLayout} title={layout === 'list' ? 'Switch to grid' : 'Switch to list'}>
                {layout === 'list' ? <LayoutGrid size={14} /> : <List size={14} />}
              </button>
              <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>
                + New Notebook
              </button>
            </div>
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
          {createError && <p style={{ color: '#c0392b', fontSize: 12, margin: '0 0 12px' }}>{createError}</p>}

          {notebooks.length === 0 && !creating ? (
            <EmptyState
              icon={<BookOpen size={32} />}
              title="No notebooks yet"
              text="Create your first notebook to start querying data."
              action={{ label: 'Create your first notebook', onClick: () => setCreating(true) }}
            />
          ) : (
            <div style={layout === 'grid' ? styles.grid : styles.list}>
              {notebooks.map((nb) =>
                layout === 'grid'
                  ? <NotebookCard key={nb.id} notebook={nb} onDelete={() => deleteNotebook.mutate(nb.id)} />
                  : <NotebookRow key={nb.id} notebook={nb} onDelete={() => deleteNotebook.mutate(nb.id)} />
              )}
            </div>
          )}
        </div>
    </AppShell>
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
          <BookOpen size={20} style={{ color: 'var(--accent)' }} />
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

function NotebookRow({ notebook, onDelete }: { notebook: Notebook; onDelete: () => void }) {
  const updated = new Date(notebook.updated_at)
  const dateStr = updated.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })

  return (
    <div style={rowStyles.row}>
      <Link to={`/notebooks/${notebook.id}`} style={rowStyles.link}>
        <BookOpen size={18} style={{ color: 'var(--accent)', flexShrink: 0 }} />
        <div style={rowStyles.info}>
          <span style={rowStyles.title}>{notebook.title}</span>
          {notebook.description && <span style={rowStyles.desc}>{notebook.description}</span>}
        </div>
        <span style={rowStyles.date}>{dateStr}</span>
      </Link>
      <button type="button" style={rowStyles.del} onClick={(e) => { e.preventDefault(); onDelete() }}>Delete</button>
    </div>
  )
}

const rowStyles: Record<string, React.CSSProperties> = {
  row: { display: 'flex', alignItems: 'center', background: 'white', borderRadius: 8, border: '1px solid var(--border)', padding: '10px 16px', gap: 12 },
  link: { flex: 1, display: 'flex', alignItems: 'center', gap: 12, textDecoration: 'none' },
  icon: { fontSize: 18, color: 'var(--accent)', flexShrink: 0 },
  info: { flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 },
  title: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  desc: { fontSize: 12, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  date: { fontSize: 12, color: 'var(--text-muted)', flexShrink: 0 },
  del: { padding: '3px 8px', border: 'none', background: 'transparent', color: 'var(--error)', fontSize: 12, cursor: 'pointer', flexShrink: 0 },
}

const styles: Record<string, React.CSSProperties> = {
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
  layoutBtn: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 6, background: 'none', cursor: 'pointer', fontSize: 14 },
  list: { display: 'flex', flexDirection: 'column', gap: 8 },
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
