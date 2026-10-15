import { useState, useEffect, useRef } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard } from '../types'
import { favoritesQueryKey, useFavorites } from '../hooks/useFavorites'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { FavoriteButton } from '../components/FavoriteButton'
import { Skeleton } from '../components/Skeleton'
import { SectionHeader } from '../components/SectionHeader'
import { FormModal } from '../components/FormModal'
import { RowAction } from '../components/RowActions'
import { Bookmark, LayoutGrid, List, LayoutDashboard, Pencil, Trash2 } from 'lucide-react'
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
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  // The favorites filter lives in the URL (`?fav=1`) like the Files page's
  // chip, so it survives refresh/back; filtering itself is client-side over
  // the shared favorites id set — no endpoint.
  const favoritesOnly = searchParams.get('fav') === '1'
  const [newTitle, setNewTitle] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const titleRef = useRef<HTMLInputElement>(null)
  const [layout, setLayout] = useState<'grid' | 'list'>(() =>
    (localStorage.getItem('aether_dashboards_layout') as 'grid' | 'list') ?? 'list'
  )
  const toggleLayout = () => {
    const next = layout === 'list' ? 'grid' : 'list'
    setLayout(next)
    localStorage.setItem('aether_dashboards_layout', next)
  }

  const toggleFavorites = () => {
    setSearchParams((current) => {
      const params = new URLSearchParams(current)
      if (params.get('fav') === '1') params.delete('fav')
      else params.set('fav', '1')
      return params
    })
  }

  const { data: dashboards = [], isLoading } = useQuery({
    queryKey: ['dashboards'],
    queryFn: () => api.get<Dashboard[]>('/api/v1/dashboards'),
  })
  const {
    ids: favoriteIds,
    isLoading: favoritesLoading,
    isError: favoritesError,
    hasData: favoritesHasData,
    isFetching: favoritesFetching,
  } = useFavorites()
  // A data-less refetch flips the query back to "loading" and clears the
  // error, so the flag keeps the error panel (and its pending feedback) up
  // from the Retry click until the retry settles.
  const [favoritesRetrying, setFavoritesRetrying] = useState(false)
  // Never filter on an unknown id set (a still-loading first fetch), but a
  // failed *refetch* keeps the cached id set usable: only a data-less failure
  // — the first fetch, or a retry of it — is an error state.
  const favoritesUnavailable = !favoritesHasData && (favoritesError || favoritesRetrying)
  const favoriteDashboards = dashboards.filter((d) => favoriteIds.dashboard.has(d.id))
  const visibleDashboards = favoritesOnly && !favoritesLoading && !favoritesUnavailable
    ? favoriteDashboards
    : dashboards
  // The header count follows what is listed: the filtered set while the chip
  // is on, the catalog otherwise.
  const listedCount = favoritesOnly ? favoriteDashboards.length : dashboards.length
  const subtitle = listedCount > 0 ? `${listedCount} dashboard${listedCount !== 1 ? 's' : ''}` : ''

  const retryFavorites = () => {
    setFavoritesRetrying(true)
    void qc.invalidateQueries({ queryKey: favoritesQueryKey })
      // The failure is already rendered from the query state; the promise is
      // only the settle signal for the flag.
      .catch(() => {})
      .finally(() => setFavoritesRetrying(false))
  }

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
        <SectionHeader title="Dashboards" subtitle={subtitle}>
          <span style={styles.headerActions}>
            <button
              type="button"
              style={{ ...styles.chip, ...(favoritesOnly ? styles.chipActive : {}) }}
              aria-pressed={favoritesOnly}
              title={favoritesOnly ? 'Show all dashboards' : 'Show only favorites'}
              onClick={toggleFavorites}
            >
              <Bookmark size={12} fill={favoritesOnly ? 'currentColor' : 'none'} aria-hidden="true" />
              Favorites
            </button>
            <button type="button" style={styles.layoutBtn} onClick={toggleLayout} title={layout === 'list' ? 'Switch to grid view' : 'Switch to list view'} aria-label={layout === 'list' ? 'Switch to grid view' : 'Switch to list view'}>
              {layout === 'list' ? <LayoutGrid size={14} /> : <List size={14} />}
            </button>
            <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Dashboard</button>
          </span>
        </SectionHeader>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: -16, marginBottom: 24 }}>
          Build real-time visual dashboards from your notebook query results.
        </p>

        {creating && (
          <FormModal
            title="New Dashboard"
            onClose={() => { setCreating(false); setNewTitle(''); setCreateError(null) }}
            initialFocusRef={titleRef}
            width="min(420px, 92vw)"
            error={createError}
            submitLabel="Create"
            pendingLabel="Creating…"
            pending={createDashboard.isPending}
            submitDisabled={!newTitle.trim()}
            onSubmit={() => createDashboard.mutate()}
          >
            <label style={styles.label}>Title
              <input
                ref={titleRef}
                style={styles.createInput}
                placeholder="Dashboard title"
                value={newTitle}
                onChange={(e) => setNewTitle(e.target.value)}
              />
            </label>
          </FormModal>
        )}
        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}

        {isLoading || (favoritesOnly && favoritesLoading && !favoritesUnavailable) ? (
          <div style={{ padding: '8px 0' }}>
            <Skeleton count={4} height={48} />
          </div>
        ) : favoritesOnly && favoritesUnavailable ? (
          <div role="alert" style={styles.favoritesError}>
            <span>Couldn't load favorites.</span>
            <button
              type="button"
              style={{ ...styles.retryBtn, ...(favoritesFetching || favoritesRetrying ? styles.retryBtnBusy : {}) }}
              disabled={favoritesFetching || favoritesRetrying}
              onClick={retryFavorites}
            >
              {favoritesFetching || favoritesRetrying ? 'Retrying…' : 'Retry'}
            </button>
          </div>
        ) : favoritesOnly && visibleDashboards.length === 0 ? (
          <EmptyState
            title="No favorite dashboards"
            text="Bookmark a dashboard with the bookmark icon to see it here."
          />
        ) : dashboards.length === 0 ? (
          <EmptyState
            title="No dashboards yet"
            text="Use the button above to create a dashboard and display notebook cell outputs in a shared view."
          />
        ) : (
          <div style={layout === 'grid' ? styles.grid : styles.list}>
            {visibleDashboards.map((d) =>
              layout === 'grid'
                ? (
                  <div key={d.id} style={styles.card} className="card-hover">
                    <Link to={`/dashboards/${d.id}/view`} style={styles.cardLink}>
                      <div style={styles.cardIcon}><LayoutDashboard size={24} style={{ opacity: 0.4 }} /></div>
                      <div style={styles.cardTitle}>{d.title}</div>
                      <div style={styles.cardMeta}>Updated {fmtDate(d.updated_at)}</div>
                      {d.public_token && <div style={styles.publicBadge}>Public</div>}
                    </Link>
                    <span style={styles.cardActions}>
                      {/* Cards have no hover column to hide behind: the bookmark stays visible. */}
                      <FavoriteButton resourceType="dashboard" resourceId={d.id} />
                      <RowAction label="Edit dashboard" title="Edit layout and widgets" accent icon={<Pencil size={13} />} onClick={() => navigate(`/dashboards/${d.id}`)} />
                      <RowAction label="Delete dashboard" icon={<Trash2 size={13} />} danger onClick={() => setDeleteTarget(d)} />
                    </span>
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
  const navigate = useNavigate()
  return (
    <div style={rowStyles.row} className="card-hover favorite-host">
      <Link to={`/dashboards/${dashboard.id}/view`} style={rowStyles.link}>
        <LayoutDashboard size={18} style={{ color: 'var(--accent)', flexShrink: 0 }} />
        <div style={rowStyles.info}>
          <span style={rowStyles.title}>{dashboard.title}</span>
          {dashboard.public_token && <span style={rowStyles.badge}>Public</span>}
        </div>
        <span style={rowStyles.date}>{fmtDate(dashboard.updated_at)}</span>
      </Link>
      <FavoriteButton resourceType="dashboard" resourceId={dashboard.id} className="favorite-reveal" />
      <RowAction label="Edit dashboard" title="Edit layout and widgets" accent icon={<Pencil size={13} />} onClick={() => navigate(`/dashboards/${dashboard.id}`)} />
      <RowAction label="Delete dashboard" icon={<Trash2 size={13} />} danger onClick={onRequestDelete} />
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
}

const styles: Record<string, React.CSSProperties> = {
  layoutBtn: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', fontSize: 14, color: 'var(--text-secondary)' },
  newBtn: { padding: '7px 16px', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  headerActions: { display: 'inline-flex', alignItems: 'center', gap: 8 },
  // Quiet accent for the active chip: accent ink on a hairline, no fill
  // (matches the Files page's chip).
  chip: { display: 'inline-flex', alignItems: 'center', gap: 6, padding: '4px 12px', background: 'none', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--border)', borderRadius: 999, color: 'var(--text-secondary)', fontSize: 12, fontWeight: 500, cursor: 'pointer', whiteSpace: 'nowrap' },
  chipActive: { borderColor: 'var(--accent)', color: 'var(--accent)' },
  favoritesError: { display: 'flex', alignItems: 'center', gap: 12, padding: '16px', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, color: 'var(--text-secondary)' },
  retryBtn: { background: 'none', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12, padding: '3px 10px' },
  retryBtnBusy: { opacity: 0.6, cursor: 'default' },
  body: { maxWidth: 1280, margin: '0 auto', padding: '40px 40px', width: '100%' },
  list: { display: 'flex', flexDirection: 'column', gap: 8 },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  createInput: { padding: '8px 12px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 14, fontFamily: 'var(--font-sans)', background: 'var(--bg-input)', color: 'var(--text-primary)' },
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
  cardActions: {
    position: 'absolute',
    top: 10,
    right: 10,
    display: 'flex',
    gap: 4,
  },
}
