import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard } from '../types'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { Skeleton } from '../components/Skeleton'
import { SectionHeader } from '../components/SectionHeader'
import { LayoutGrid, List, LayoutDashboard, X } from 'lucide-react'
import { ConfirmDialog } from '../components/ConfirmDialog'

const fmtDate = (d: string) => {
  const date = new Date(d)
  const today = new Date()
  if (date.toDateString() === today.toDateString()) {
    return `Today ${date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
  }
  return date.toLocaleString('en-US', { month: 'short', day: 'numeric', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function DashboardsPage() {
  useEffect(() => { document.title = "Dashboards — Aether Notebooks" }, [])
  const qc = useQueryClient()
  const [newTitle, setNewTitle] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [layout, setLayout] = useState<'grid' | 'list'>(() =>
    (localStorage.getItem('aether_dashboards_layout') as 'grid' | 'list') ?? 'list'
  )
  const toggleLayout = () => {
    const next = layout === 'list' ? 'grid' : 'list'
    setLayout(next)
    localStorage.setItem('aether_dashboards_layout', next)
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
  const [deleteTarget, setDeleteTarget] = useState<Dashboard | null>(null)

  const deleteDashboard = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/dashboards/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dashboards'] }),
    onError: (err: Error) => setDeleteError(err.message),
  })

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader title="Dashboards" subtitle={dashboards.length > 0 ? `${dashboards.length} dashboard${dashboards.length !== 1 ? 's' : ''}` : ''}>
          <button type="button" style={styles.layoutBtn} onClick={toggleLayout} title={layout === 'list' ? 'Switch to grid view' : 'Switch to list view'} aria-label={layout === 'list' ? 'Switch to grid view' : 'Switch to list view'}>
            {layout === 'list' ? <LayoutGrid size={14} /> : <List size={14} />}
          </button>
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Dashboard</button>
        </SectionHeader>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: -16, marginBottom: 24 }}>
          Build real-time visual dashboards from your notebook query results.
        </p>

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
          <div style={{ padding: '8px 0' }}>
            <Skeleton count={4} height={48} />
          </div>
        ) : dashboards.length === 0 ? (
          <EmptyState
            title="No dashboards yet"
            text="Use the button above to create a dashboard and display notebook cell outputs in a shared view."
          />
        ) : (
          <div style={layout === 'grid' ? styles.grid : styles.list}>
            {dashboards.map((d) =>
              layout === 'grid'
                ? (
                  <div key={d.id} style={styles.card} className="card-hover">
                    <Link to={`/dashboards/${d.id}`} style={styles.cardLink}>
                      <div style={styles.cardIcon}><LayoutDashboard size={24} style={{ opacity: 0.4 }} /></div>
                      <div style={styles.cardTitle}>{d.title}</div>
                      <div style={styles.cardMeta}>Updated {fmtDate(d.updated_at)}</div>
                      {d.public_token && <div style={styles.publicBadge}>Public</div>}
                    </Link>
                    <button type="button" style={styles.deleteBtn} onClick={() => setDeleteTarget(d)} title="Delete dashboard"><X size={13} /></button>
                  </div>
                )
                : <DashboardRow key={d.id} dashboard={d} onRequestDelete={() => setDeleteTarget(d)} />
            )}
          </div>
        )}
      </div>
      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete dashboard"
        message={`Delete "${deleteTarget?.title}"? It will be moved to trash and automatically deleted after 7 days.`}
        confirmLabel="Delete"
        destructive
        onConfirm={() => { if (deleteTarget) deleteDashboard.mutate(deleteTarget.id); setDeleteTarget(null) }}
        onCancel={() => setDeleteTarget(null)}
      />
    </AppShell>
  )
}

function DashboardRow({ dashboard, onRequestDelete }: { dashboard: Dashboard; onRequestDelete: () => void }) {
  return (
    <div style={rowStyles.row} className="card-hover">
      <Link to={`/dashboards/${dashboard.id}`} style={rowStyles.link}>
        <LayoutDashboard size={18} style={{ color: 'var(--accent)', flexShrink: 0 }} />
        <div style={rowStyles.info}>
          <span style={rowStyles.title}>{dashboard.title}</span>
          {dashboard.public_token && <span style={rowStyles.badge}>Public</span>}
        </div>
        <span style={rowStyles.date}>{fmtDate(dashboard.updated_at)}</span>
      </Link>
      <button type="button" style={rowStyles.del} onClick={(e) => { e.preventDefault(); onRequestDelete() }}>Delete</button>
    </div>
  )
}

const rowStyles: Record<string, React.CSSProperties> = {
  row: { display: 'flex', alignItems: 'center', background: 'var(--bg-card)', borderRadius: 4, border: '1px solid var(--border)', padding: '10px 16px', gap: 12, transition: 'border-color 0.15s' },
  link: { flex: 1, display: 'flex', alignItems: 'center', gap: 12, textDecoration: 'none' },
  icon: { fontSize: 18, color: 'var(--accent)', flexShrink: 0 },
  info: { flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', gap: 8 },
  title: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  badge: { fontSize: 10, fontWeight: 700, color: 'var(--text-secondary)', background: 'var(--accent-light)', padding: '2px 6px', borderRadius: 3, flexShrink: 0 },
  date: { fontSize: 12, color: 'var(--text-secondary)', flexShrink: 0 },
  del: { padding: '3px 8px', border: 'none', background: 'transparent', color: 'var(--error)', fontSize: 12, cursor: 'pointer', flexShrink: 0 },
}

const styles: Record<string, React.CSSProperties> = {
  layoutBtn: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', fontSize: 14, color: 'var(--text-secondary)' },
  newBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  body: { maxWidth: 1280, margin: '0 auto', padding: '40px 40px', width: '100%' },
  list: { display: 'flex', flexDirection: 'column', gap: 8 },
  createForm: { display: 'flex', gap: 8, marginBottom: 24, alignItems: 'center', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: 4, padding: '12px 16px' },
  createInput: { flex: 1, maxWidth: 360, padding: '8px 12px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 14, fontFamily: 'var(--font-sans)', background: 'var(--bg-input)', color: 'var(--text-primary)' },
  createBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  cancelBtn: { padding: '7px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  grid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 16 },
  card: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: 20,
    position: 'relative',
    transition: 'border-color 0.15s',
  },
  cardLink: { textDecoration: 'none', display: 'block' },
  cardIcon: { fontSize: 24, marginBottom: 10, opacity: 0.4 },
  cardTitle: { fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 6 },
  cardMeta: { fontSize: 12, color: 'var(--text-secondary)' },
  publicBadge: { marginTop: 8, display: 'inline-block', fontSize: 10, fontWeight: 700, letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--text-secondary)', background: 'var(--accent-light)', padding: '2px 7px', borderRadius: 3 },
  deleteBtn: {
    position: 'absolute',
    top: 10,
    right: 10,
    background: 'transparent',
    border: 'none',
    fontSize: 13,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    padding: '3px 6px',
    borderRadius: 4,
  },
}
