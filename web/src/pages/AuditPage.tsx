import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { AuditEntry } from '../types'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { SectionHeader } from '../components/SectionHeader'
import { StyledTable } from '../components/StyledTable'
import { ErrorBanner } from '../components/ErrorBanner'

const PAGE_SIZE = 50

export function AuditPage() {
  useEffect(() => { document.title = "Audit — Heaven's Notebooks" }, [])
  const [offset, setOffset] = useState(0)
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [actionFilter, setActionFilter] = useState('')
  const [resourceTypeFilter, setResourceTypeFilter] = useState('')
  const [hasMore, setHasMore] = useState(true)

  useEffect(() => { setOffset(0); setEntries([]) }, [resourceTypeFilter, actionFilter])

  const { data: page, isFetching, isLoading, error } = useQuery({
    queryKey: ['audit', offset, resourceTypeFilter, actionFilter],
    queryFn: () => {
      const params = new URLSearchParams({
        limit: String(PAGE_SIZE),
        offset: String(offset),
      })
      if (resourceTypeFilter) params.set('resource_type', resourceTypeFilter)
      if (actionFilter.trim()) params.set('action', actionFilter.trim())
      return api.get<AuditEntry[]>(`/api/v1/audit?${params}`)
    },
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

  const filtered = entries

  function handleLoadMore() {
    setOffset((prev) => prev + PAGE_SIZE)
  }

  return (
    <AppShell>
        <div style={styles.content}>
          <SectionHeader title="Audit Log" subtitle={`${filtered.length} entr${filtered.length !== 1 ? 'ies' : 'y'} loaded`}>
            <select
              style={{ ...styles.filterInput, maxWidth: 160, cursor: 'pointer' }}
              value={resourceTypeFilter}
              onChange={(e) => setResourceTypeFilter(e.target.value)}
              aria-label="Filter by resource type"
            >
              <option value="">All types</option>
              <option value="notebook">Notebook</option>
              <option value="cell">Cell</option>
              <option value="dashboard">Dashboard</option>
              <option value="connector">Connector</option>
              <option value="user">User</option>
            </select>
            <input
              style={styles.filterInput}
              value={actionFilter}
              onChange={(e) => setActionFilter(e.target.value)}
              placeholder="Filter by action…"
            />
          </SectionHeader>

          {error && <ErrorBanner message={`Failed to load audit log: ${(error as Error).message}`} />}
          {isLoading ? (
            <div style={styles.state}>
              <p style={styles.stateText}>Loading audit log…</p>
            </div>
          ) : filtered.length === 0 ? (
            <EmptyState
              icon={<span>▦</span>}
              title="No entries found"
              text={(actionFilter || resourceTypeFilter) ? 'No entries match the selected filters.' : 'The audit log is empty.'}
            />
          ) : (
            <StyledTable
              headers={['Timestamp', 'Action', 'Resource Type', 'Resource', 'User']}
              thStyle={{ fontSize: 12, background: 'var(--bg-primary)', letterSpacing: 'normal', borderBottom: '1px solid var(--border)' }}
            >
              {filtered.map((entry) => (
                <AuditRow key={entry.id} entry={entry} />
              ))}
            </StyledTable>
          )}

          {!isLoading && hasMore && !actionFilter && !resourceTypeFilter && (
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
    </AppShell>
  )
}

function truncateId(id: string) {
  return id.length > 8 ? `${id.slice(0, 8)}…` : id
}

function ResourceCell({ entry }: { entry: AuditEntry }) {
  const { resource_type, resource_id, resource_name, resource_parent_name } = entry

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (resource_id) navigator.clipboard.writeText(resource_id)
  }

  if (resource_type === 'cell') {
    const parent = resource_parent_name || null
    const id = resource_id ? truncateId(resource_id) : null
    if (parent && id) {
      return <span>{parent} <span style={styles.resourceSub}>› {id}</span> <button type="button" onClick={handleCopy} style={styles.copyBtn} title={`Copy ID: ${resource_id}`}>📋</button></span>
    }
    if (parent) return <span>{parent}</span>
    return <span style={styles.mono}>{id || '—'}</span>
  }

  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
      {resource_name && <span>{resource_name}</span>}
      {resource_id && (
        <span style={resource_name ? styles.resourceSub : styles.mono}>
          {truncateId(resource_id)}
        </span>
      )}
      {resource_id && (
        <button type="button" onClick={handleCopy} style={styles.copyBtn} title={`Copy full ID: ${resource_id}`}>📋</button>
      )}
    </span>
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
      <td style={styles.td} title={entry.resource_id}>
        <ResourceCell entry={entry} />
      </td>
      <td style={styles.td} title={entry.user_id}>
        <span>{entry.user_email || entry.user_id || '—'}</span>
        {entry.metadata?.ip ? (
          <span style={styles.resourceSub}> · {String(entry.metadata.ip)}</span>
        ) : null}
      </td>
    </tr>
  )
}

const styles: Record<string, React.CSSProperties> = {
  content: {
    maxWidth: 1280,
    margin: '0 auto',
  },
  filterInput: {
    padding: '8px 12px',
    border: '1.5px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    outline: 'none',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
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
  stateText: {
    fontSize: 14,
    color: 'var(--text-secondary)',
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
  resourceSub: {
    fontFamily: 'monospace',
    fontSize: 11,
    color: 'var(--text-muted)',
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
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    color: 'var(--text-primary)',
    transition: 'border-color 0.15s',
  },
  copyBtn: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    fontSize: 11,
    padding: '0 2px',
    verticalAlign: 'middle',
    opacity: 0.5,
  },
}
