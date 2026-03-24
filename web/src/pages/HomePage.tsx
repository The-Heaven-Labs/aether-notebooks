import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api/client'
import { Notebook } from '../types'
import { useAuth } from '../hooks/useAuth'

export function HomePage() {
  const { logout } = useAuth()
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
      <div style={styles.header}>
        <div style={styles.brand}>
          <span style={styles.logo}>📓</span>
          <span style={styles.brandName}>Heaven's Notebooks</span>
        </div>
        <button style={styles.logoutBtn} onClick={logout}>Sign Out</button>
      </div>

      <div style={styles.content}>
        <div style={styles.sectionHeader}>
          <h2 style={styles.sectionTitle}>Notebooks</h2>
          <button style={styles.newBtn} onClick={() => setCreating(true)}>+ New Notebook</button>
        </div>

        {creating && (
          <div style={styles.createForm}>
            <input
              style={styles.input}
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
              style={styles.createBtn}
              disabled={!newTitle.trim()}
              onClick={() => createNotebook.mutate(newTitle.trim())}
            >
              Create
            </button>
            <button style={styles.cancelBtn} onClick={() => setCreating(false)}>Cancel</button>
          </div>
        )}

        {notebooks.length === 0 && !creating ? (
          <div style={styles.empty}>
            <p>No notebooks yet.</p>
            <button style={styles.newBtn} onClick={() => setCreating(true)}>Create your first notebook</button>
          </div>
        ) : (
          <div style={styles.grid}>
            {notebooks.map((nb) => (
              <div key={nb.id} style={styles.card}>
                <Link to={`/notebooks/${nb.id}`} style={styles.cardTitle}>{nb.title}</Link>
                <p style={styles.cardMeta}>
                  Updated {new Date(nb.updated_at).toLocaleDateString()}
                </p>
                <button
                  style={styles.deleteBtn}
                  onClick={() => deleteNotebook.mutate(nb.id)}
                >
                  Delete
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: { minHeight: '100vh', background: 'var(--bg-primary)' },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 32px',
    borderBottom: '1px solid var(--border)',
    background: 'white',
  },
  brand: { display: 'flex', alignItems: 'center', gap: 10 },
  logo: { fontSize: 22 },
  brandName: { fontSize: 18, fontWeight: 700, color: 'var(--accent)' },
  logoutBtn: {
    padding: '6px 14px',
    border: '1px solid var(--border)',
    borderRadius: 6,
    background: 'none',
    fontSize: 13,
    cursor: 'pointer',
  },
  content: { maxWidth: 1100, margin: '0 auto', padding: '40px 24px' },
  sectionHeader: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 },
  sectionTitle: { fontSize: 20, fontWeight: 700 },
  newBtn: {
    padding: '7px 16px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 13,
    cursor: 'pointer',
  },
  createForm: { display: 'flex', gap: 10, marginBottom: 20 },
  input: {
    flex: 1,
    padding: '8px 12px',
    border: '1px solid var(--border)',
    borderRadius: 6,
    fontSize: 14,
    outline: 'none',
  },
  createBtn: {
    padding: '8px 16px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 13,
    cursor: 'pointer',
  },
  cancelBtn: {
    padding: '8px 16px',
    border: '1px solid var(--border)',
    borderRadius: 6,
    background: 'none',
    fontSize: 13,
    cursor: 'pointer',
  },
  empty: { textAlign: 'center', padding: '60px 0', color: 'var(--text-secondary)', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 16 },
  grid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 16 },
  card: {
    padding: '20px',
    background: 'white',
    borderRadius: 10,
    border: '1px solid var(--border)',
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  cardTitle: { fontSize: 16, fontWeight: 600, color: 'var(--accent)' },
  cardMeta: { fontSize: 12, color: 'var(--text-secondary)' },
  deleteBtn: {
    alignSelf: 'flex-start',
    padding: '4px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'none',
    fontSize: 12,
    cursor: 'pointer',
    color: 'var(--error)',
    marginTop: 4,
  },
}
