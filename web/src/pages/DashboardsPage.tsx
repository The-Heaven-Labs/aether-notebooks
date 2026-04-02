import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard } from '../types'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { SectionHeader } from '../components/SectionHeader'
import { LayoutGrid, List, LayoutDashboard, X } from 'lucide-react'

const fmtDate = (d: string) => {
  const date = new Date(d)
  const today = new Date()
  return date.toDateString() === today.toDateString()
    ? `Today ${date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
    : date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

export function DashboardsPage() {
  useEffect(() => { document.title = "Dashboards — Heaven's Notebooks" }, [])
  const qc = useQueryClient()
  const [newTitle, setNewTitle] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [layout, setLayout] = useState<'grid' | 'list'>(() =>
    (localStorage.getItem('hnb_dashboards_layout') as 'grid' | 'list') ?? 'list'
  )
  const toggleLayout = () => {
    const next = layout === 'list' ? 'grid' : 'list'
    setLayout(next)
    localStorage.setItem('hnb_dashboards_layout', next)
  }

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

  const [deleteError, setDeleteError] = useState<string | null>(null)

  const deleteDashboard = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/dashboards/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dashboards'] }),
    onError: (err: Error) => setDeleteError(err.message),
  })

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader title="Dashboards" subtitle={`${dashboards.length} dashboard${dashboards.length !== 1 ? 's' : ''}`}>
          <button type="button" style={styles.layoutBtn} onClick={toggleLayout} title={layout === 'list' ? 'Switch to grid' : 'Switch to list'}>
            {layout === 'list' ? <LayoutGrid size={14} /> : <List size={14} />}
          </button>
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Dashboard</button>
        </SectionHeader>

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
        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}

        {isLoading ? (
          <div style={styles.loading} />
        ) : dashboards.length === 0 ? (
          <EmptyState
            title="No dashboards yet"
            text="Create a dashboard to display notebook cell outputs in a shared view."
            action={{ label: '+ New Dashboard', onClick: () => setCreating(true) }}
          />
        ) : (
          <div style={layout === 'grid' ? styles.grid : styles.list}>
            {dashboards.map((d) =>
              layout === 'grid'
                ? (
                  <div key={d.id} style={styles.card}>
                    <Link to={`/dashboards/${d.id}`} style={styles.cardLink}>
                      <div style={styles.cardIcon}><LayoutDashboard size={24} style={{ opacity: 0.4 }} /></div>
                      <div style={styles.cardTitle}>{d.title}</div>
                      <div style={styles.cardMeta}>Updated {fmtDate(d.updated_at)}</div>
                      {d.public_token && <div style={styles.publicBadge}>Public</div>}
                    </Link>
                    <button type="button" style={styles.deleteBtn} onClick={() => { if (confirm(`Delete "${d.title}"?`)) deleteDashboard.mutate(d.id) }} title="Delete dashboard"><X size={13} /></button>
                  </div>
                )
                : <DashboardRow key={d.id} dashboard={d} onDelete={() => deleteDashboard.mutate(d.id)} />
            )}
          </div>
        )}
      </div>
    </AppShell>
  )
}

function DashboardRow({ dashboard, onDelete }: { dashboard: Dashboard; onDelete: () => void }) {
  return (
    <div style={rowStyles.row}>
      <Link to={`/dashboards/${dashboard.id}`} style={rowStyles.link}>
        <LayoutDashboard size={18} style={{ color: 'var(--accent)', flexShrink: 0 }} />
        <div style={rowStyles.info}>
          <span style={rowStyles.title}>{dashboard.title}</span>
          {dashboard.public_token && <span style={rowStyles.badge}>Public</span>}
        </div>
        <span style={rowStyles.date}>{fmtDate(dashboard.updated_at)}</span>
      </Link>
      <button type="button" style={rowStyles.del} onClick={(e) => { e.preventDefault(); if (confirm(`Delete "${dashboard.title}"?`)) onDelete() }}>Delete</button>
    </div>
  )
}

const rowStyles: Record<string, React.CSSProperties> = {
  row: { display: 'flex', alignItems: 'center', background: 'white', borderRadius: 8, border: '1px solid var(--border)', padding: '10px 16px', gap: 12 },
  link: { flex: 1, display: 'flex', alignItems: 'center', gap: 12, textDecoration: 'none' },
  icon: { fontSize: 18, color: 'var(--accent)', flexShrink: 0 },
  info: { flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', gap: 8 },
  title: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  badge: { fontSize: 10, fontWeight: 700, color: 'var(--accent)', background: '#f0edff', padding: '2px 6px', borderRadius: 4, flexShrink: 0 },
  date: { fontSize: 12, color: 'var(--text-muted)', flexShrink: 0 },
  del: { padding: '3px 8px', border: 'none', background: 'transparent', color: 'var(--error)', fontSize: 12, cursor: 'pointer', flexShrink: 0 },
}

const styles: Record<string, React.CSSProperties> = {
  layoutBtn: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 6, background: 'none', cursor: 'pointer', fontSize: 14 },
  newBtn: { padding: '6px 16px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  body: { maxWidth: 1280, margin: '0 auto', padding: '40px 40px', width: '100%' },
  list: { display: 'flex', flexDirection: 'column', gap: 8 },
  createForm: { display: 'flex', gap: 8, marginBottom: 24, alignItems: 'center' },
  createInput: { flex: 1, maxWidth: 360, padding: '8px 12px', border: '1px solid var(--border)', borderRadius: 6, fontSize: 14, fontFamily: 'var(--font-sans)', background: 'white' },
  createBtn: { padding: '8px 20px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  cancelBtn: { padding: '8px 16px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 6, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  loading: { width: 8, height: 8, borderRadius: '50%', background: 'var(--accent)', opacity: 0.5, margin: '80px auto' },
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
  publicBadge: { marginTop: 8, display: 'inline-block', fontSize: 10, fontWeight: 700, letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--accent)', background: '#f0edff', padding: '2px 7px', borderRadius: 4 },
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
