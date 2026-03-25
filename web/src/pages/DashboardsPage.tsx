import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import type { Dashboard } from '../types'

const fmtDate = (d: string) => {
  const date = new Date(d)
  const today = new Date()
  return date.toDateString() === today.toDateString()
    ? `Today ${date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
    : date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

export function DashboardsPage() {
  const qc = useQueryClient()
  const { logout } = useAuth()
  const [newTitle, setNewTitle] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const { data: dashboards = [], isLoading } = useQuery({
    queryKey: ['dashboards'],
    queryFn: () => api.get<Dashboard[]>('/api/v1/dashboards'),
  })

  const createDashboard = useMutation({
    mutationFn: () => api.post<Dashboard>('/api/v1/dashboards', { title: newTitle.trim(), settings: {} }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['dashboards'] })
      setNewTitle('')
      setCreating(false)
      setCreateError(null)
    },
    onError: (err: Error) => setCreateError(err.message),
  })

  const deleteDashboard = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/dashboards/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dashboards'] }),
  })

  return (
    <div style={styles.page}>
      <header style={styles.header}>
        <div style={styles.headerLeft}>
          <Link to="/" style={styles.navLink}>Notebooks</Link>
          <span style={styles.navSep}>·</span>
          <span style={styles.navActive}>Dashboards</span>
          <span style={styles.navSep}>·</span>
          <Link to="/connectors" style={styles.navLink}>Connectors</Link>
        </div>
        <div style={styles.headerRight}>
          <button style={styles.newBtn} onClick={() => setCreating(true)}>+ New Dashboard</button>
          <button style={styles.signOutBtn} onClick={logout}>Sign out</button>
        </div>
      </header>

      <div style={styles.body}>
        {creating && (
          <form
            style={styles.createForm}
            onSubmit={(e) => { e.preventDefault(); if (newTitle.trim()) createDashboard.mutate() }}
          >
            <input
              style={styles.createInput}
              placeholder="Dashboard title"
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              autoFocus
            />
            <button type="submit" style={styles.createBtn} disabled={!newTitle.trim() || createDashboard.isPending}>
              Create
            </button>
            <button type="button" style={styles.cancelBtn} onClick={() => { setCreating(false); setNewTitle('') }}>
              Cancel
            </button>
          </form>
        )}
        {createError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{createError}</p>}

        {isLoading ? (
          <div style={styles.loading} />
        ) : dashboards.length === 0 ? (
          <div style={styles.empty}>
            <p style={styles.emptyText}>No dashboards yet</p>
            <p style={styles.emptySubtext}>Create a dashboard to display notebook cell outputs in a shared view.</p>
            <button style={styles.newBtn} onClick={() => setCreating(true)}>+ New Dashboard</button>
          </div>
        ) : (
          <div style={styles.grid}>
            {dashboards.map((d) => (
              <div key={d.id} style={styles.card}>
                <Link to={`/dashboards/${d.id}`} style={styles.cardLink}>
                  <div style={styles.cardIcon}>⊟</div>
                  <div style={styles.cardTitle}>{d.title}</div>
                  <div style={styles.cardMeta}>Updated {fmtDate(d.updated_at)}</div>
                  {d.public_token && (
                    <div style={styles.publicBadge}>Public</div>
                  )}
                </Link>
                <button
                  style={styles.deleteBtn}
                  onClick={() => { if (confirm(`Delete "${d.title}"?`)) deleteDashboard.mutate(d.id) }}
                  title="Delete dashboard"
                >
                  ✕
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
  page: { minHeight: '100vh', background: 'var(--bg-primary)', display: 'flex', flexDirection: 'column' },
  header: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    height: 52,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 32px',
    position: 'sticky',
    top: 0,
    zIndex: 100,
    flexShrink: 0,
  },
  headerLeft: { display: 'flex', alignItems: 'center', gap: 12 },
  navLink: { color: '#6a6260', textDecoration: 'none', fontSize: 13, fontWeight: 500 },
  navActive: { color: 'var(--nav-text)', fontSize: 13, fontWeight: 600 },
  navSep: { color: '#3a3630', fontSize: 12 },
  headerRight: { display: 'flex', alignItems: 'center', gap: 10 },
  newBtn: { padding: '6px 16px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  signOutBtn: { padding: '6px 14px', background: 'transparent', border: '1px solid #3a3630', borderRadius: 6, fontSize: 13, color: '#8a8278', cursor: 'pointer' },
  body: { flex: 1, maxWidth: 1280, margin: '0 auto', padding: '40px 40px', width: '100%' },
  createForm: { display: 'flex', gap: 8, marginBottom: 24, alignItems: 'center' },
  createInput: { flex: 1, maxWidth: 360, padding: '8px 12px', border: '1px solid var(--border)', borderRadius: 7, fontSize: 14, fontFamily: 'var(--font-sans)', background: 'white' },
  createBtn: { padding: '8px 20px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 7, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  cancelBtn: { padding: '8px 16px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 7, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  loading: { width: 8, height: 8, borderRadius: '50%', background: 'var(--accent)', opacity: 0.5, margin: '80px auto' },
  empty: { display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', minHeight: 300, gap: 8 },
  emptyText: { fontSize: 16, fontWeight: 600, color: 'var(--text-secondary)', margin: 0 },
  emptySubtext: { fontSize: 13, color: 'var(--text-muted)', margin: '0 0 16px' },
  grid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 16 },
  card: {
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 10,
    padding: 20,
    boxShadow: 'var(--shadow-sm)',
    position: 'relative',
    transition: 'box-shadow 0.15s, border-color 0.15s',
  },
  cardLink: { textDecoration: 'none', display: 'block' },
  cardIcon: { fontSize: 24, marginBottom: 10, opacity: 0.4 },
  cardTitle: { fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 6 },
  cardMeta: { fontSize: 12, color: 'var(--text-muted)' },
  publicBadge: { marginTop: 8, display: 'inline-block', fontSize: 10, fontWeight: 700, letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--accent)', background: '#f0edff', padding: '2px 7px', borderRadius: 3 },
  deleteBtn: {
    position: 'absolute',
    top: 10,
    right: 10,
    background: 'transparent',
    border: 'none',
    fontSize: 13,
    cursor: 'pointer',
    color: 'var(--text-muted)',
    padding: '3px 6px',
    borderRadius: 4,
  },
}
