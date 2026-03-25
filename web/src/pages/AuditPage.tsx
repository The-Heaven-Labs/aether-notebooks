import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { AuditEntry } from '../types'
import { useAuth } from '../hooks/useAuth'

const PAGE_SIZE = 50

export function AuditPage() {
  const { logout } = useAuth()
  const [offset, setOffset] = useState(0)
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [actionFilter, setActionFilter] = useState('')
  const [hasMore, setHasMore] = useState(true)

  const { data: page, isFetching, isLoading, error } = useQuery({
    queryKey: ['audit', offset],
    queryFn: () => api.get<AuditEntry[]>(`/api/v1/audit?limit=${PAGE_SIZE}&offset=${offset}`),
  })

  useEffect(() => {
    if (!page) return
    if (offset === 0) {
      setEntries(page)
    } else {
      setEntries((prev) => [...prev, ...page])
    }
    setHasMore(page.length === PAGE_SIZE)
  }, [page, offset])

  const filtered = actionFilter.trim()
    ? entries.filter((e) =>
        e.action.toLowerCase().includes(actionFilter.toLowerCase())
      )
    : entries

  function handleLoadMore() {
    setOffset((prev) => prev + PAGE_SIZE)
  }

  return (
    <div style={styles.page}>
      <header style={styles.header}>
        <div style={styles.headerInner}>
          <div style={styles.brand}>
            <div style={styles.logoMark}>▦</div>
            <span style={styles.brandName}>Heaven's Notebooks</span>
          </div>
          <div style={styles.headerRight}>
            <Link to="/" style={styles.navLink}>Notebooks</Link>
            <span style={styles.navSep}>·</span>
            <Link to="/dashboards" style={styles.navLink}>Dashboards</Link>
            <span style={styles.navSep}>·</span>
            <Link to="/connectors" style={styles.navLink}>Connectors</Link>
            <span style={styles.navSep}>·</span>
            <span style={styles.navActive}>Audit</span>
            <button style={styles.logoutBtn} onClick={logout}>Sign Out</button>
          </div>
        </div>
      </header>

      <main style={styles.main}>
        <div style={styles.content}>
          <div style={styles.sectionHeader}>
            <div>
              <h2 style={styles.sectionTitle}>Audit Log</h2>
              <p style={styles.sectionSub}>{filtered.length} entr{filtered.length !== 1 ? 'ies' : 'y'} loaded</p>
            </div>
            <input
              style={styles.filterInput}
              value={actionFilter}
              onChange={(e) => setActionFilter(e.target.value)}
              placeholder="Filter by action…"
            />
          </div>

          {error && (
            <div style={styles.state}>
              <p style={{ ...styles.stateText, color: '#c0392b' }}>Failed to load audit log: {(error as Error).message}</p>
            </div>
          )}
          {isLoading ? (
            <div style={styles.state}>
              <p style={styles.stateText}>Loading audit log…</p>
            </div>
          ) : filtered.length === 0 ? (
            <div style={styles.state}>
              <div style={styles.stateIcon}>▦</div>
              <p style={styles.stateTitle}>No entries found</p>
              <p style={styles.stateText}>
                {actionFilter ? 'No entries match that action filter.' : 'The audit log is empty.'}
              </p>
            </div>
          ) : (
            <div style={styles.tableWrap}>
              <table style={styles.table}>
                <thead>
                  <tr>
                    <th style={styles.th}>Timestamp</th>
                    <th style={styles.th}>Action</th>
                    <th style={styles.th}>Resource Type</th>
                    <th style={styles.th}>Resource ID</th>
                    <th style={styles.th}>User ID</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((entry) => (
                    <AuditRow key={entry.id} entry={entry} />
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {!isLoading && hasMore && !actionFilter && (
            <div style={styles.loadMoreWrap}>
              <button
                type="button"
                style={styles.loadMoreBtn}
                onClick={handleLoadMore}
                disabled={isFetching}
              >
                {isFetching ? 'Loading…' : 'Load more'}
              </button>
            </div>
          )}
        </div>
      </main>
    </div>
  )
}

function AuditRow({ entry }: { entry: AuditEntry }) {
  const ts = new Date(entry.created_at)
  const dateStr = ts.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
  const timeStr = ts.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })

  return (
    <tr style={styles.tr}>
      <td style={styles.td}>
        <span style={styles.date}>{dateStr}</span>
        <span style={styles.time}>{timeStr}</span>
      </td>
      <td style={styles.td}>
        <span style={styles.badge}>{entry.action}</span>
      </td>
      <td style={styles.td}>
        <span style={styles.mono}>{entry.resource_type || '—'}</span>
      </td>
      <td style={styles.td}>
        <span style={styles.mono}>{entry.resource_id || '—'}</span>
      </td>
      <td style={styles.td}>
        <span style={styles.mono}>{entry.user_id || '—'}</span>
      </td>
    </tr>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: '100vh',
    background: 'var(--bg-primary)',
    display: 'flex',
    flexDirection: 'column',
  },
  header: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    flexShrink: 0,
  },
  headerInner: {
    maxWidth: 1280,
    margin: '0 auto',
    padding: '0 32px',
    height: 56,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  brand: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
  },
  logoMark: {
    width: 30,
    height: 30,
    background: 'var(--accent)',
    borderRadius: 7,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 16,
    color: 'white',
  },
  brandName: {
    fontSize: 15,
    fontWeight: 700,
    color: 'var(--nav-text)',
    letterSpacing: '-0.1px',
  },
  headerRight: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
  },
  navLink: {
    fontSize: 13,
    fontWeight: 500,
    color: '#6a6260',
    textDecoration: 'none',
  },
  navActive: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--nav-text)',
  },
  navSep: {
    fontSize: 12,
    color: '#3a3630',
  },
  logoutBtn: {
    padding: '6px 14px',
    border: '1px solid #3a3630',
    borderRadius: 6,
    background: 'transparent',
    fontSize: 13,
    color: '#8a8278',
    cursor: 'pointer',
    fontWeight: 500,
    transition: 'all 0.15s',
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
  filterInput: {
    padding: '8px 12px',
    border: '1.5px solid var(--border)',
    borderRadius: 6,
    fontSize: 13,
    outline: 'none',
    background: 'var(--bg-primary)',
    width: 220,
  },
  state: {
    textAlign: 'center',
    padding: '80px 0',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    gap: 12,
  },
  stateIcon: {
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
  stateTitle: {
    fontSize: 18,
    fontWeight: 700,
    color: 'var(--text-primary)',
    letterSpacing: '-0.2px',
  },
  stateText: {
    fontSize: 14,
    color: 'var(--text-secondary)',
  },
  tableWrap: {
    background: 'white',
    borderRadius: 10,
    border: '1px solid var(--border)',
    boxShadow: 'var(--shadow-sm)',
    overflow: 'hidden',
  },
  table: {
    width: '100%',
    borderCollapse: 'collapse',
  },
  th: {
    padding: '11px 16px',
    textAlign: 'left',
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-muted)',
    background: 'var(--bg-primary)',
    borderBottom: '1px solid var(--border)',
    letterSpacing: '0.04em',
    textTransform: 'uppercase',
  },
  tr: {
    borderBottom: '1px solid var(--border-light)',
  },
  td: {
    padding: '12px 16px',
    fontSize: 13,
    color: 'var(--text-primary)',
    verticalAlign: 'middle',
  },
  date: {
    display: 'block',
    fontWeight: 500,
    color: 'var(--text-primary)',
  },
  time: {
    display: 'block',
    fontSize: 11,
    color: 'var(--text-muted)',
    marginTop: 2,
  },
  badge: {
    display: 'inline-block',
    padding: '2px 8px',
    background: 'var(--accent-light)',
    color: 'var(--accent)',
    borderRadius: 4,
    fontSize: 12,
    fontWeight: 600,
    letterSpacing: '0.02em',
  },
  mono: {
    fontFamily: 'monospace',
    fontSize: 12,
    color: 'var(--text-secondary)',
  },
  loadMoreWrap: {
    marginTop: 20,
    display: 'flex',
    justifyContent: 'center',
  },
  loadMoreBtn: {
    padding: '9px 28px',
    background: 'white',
    border: '1.5px solid var(--border)',
    borderRadius: 7,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    color: 'var(--text-primary)',
    transition: 'border-color 0.15s',
  },
}
