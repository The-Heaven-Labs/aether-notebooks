import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { api } from '../api/client'
import type { AuditEntry } from '../types'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { SectionHeader } from '../components/SectionHeader'
import { StyledTable } from '../components/StyledTable'
import { ErrorBanner } from '../components/ErrorBanner'
import { Pagination } from '../components/Pagination'

interface AuditS3Config {
  endpoint: string; region: string; bucket: string
  use_role: boolean; batch_size: number; flush_interval_secs: number; enabled: boolean
}

const PAGE_SIZE = 50

export function AuditPage() {
  useEffect(() => { document.title = "Audit — Aether Notebooks" }, [])
  const [page, setPage] = useState(0)
  const [actionFilter, setActionFilter] = useState('')
  const [resourceTypeFilter, setResourceTypeFilter] = useState('')
  const [userFilter, setUserFilter] = useState('')
  const [dateFrom, setDateFrom] = useState('')
  const [dateTo, setDateTo] = useState('')
  const [sortCol, setSortCol] = useState<'created_at' | 'action' | 'resource_type' | ''>('')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')

  function handleSort(col: 'created_at' | 'action' | 'resource_type') {
    if (sortCol === col) {
      setSortDir(d => d === 'asc' ? 'desc' : 'asc')
    } else {
      setSortCol(col)
      setSortDir('desc')
    }
  }

  useEffect(() => { setPage(0) }, [resourceTypeFilter, actionFilter, userFilter, dateFrom, dateTo])

  const { data, isLoading, error } = useQuery({
    queryKey: ['audit', page, resourceTypeFilter, actionFilter, userFilter, dateFrom, dateTo],
    queryFn: () => {
      const params = new URLSearchParams({
        limit: String(PAGE_SIZE),
        offset: String(page * PAGE_SIZE),
      })
      if (resourceTypeFilter) params.set('resource_type', resourceTypeFilter)
      if (actionFilter.trim()) params.set('action', actionFilter.trim())
      if (userFilter.trim()) params.set('user', userFilter.trim())
      if (dateFrom) params.set('from', dateFrom)
      if (dateTo) params.set('to', dateTo)
      return api.get<{ entries: AuditEntry[]; total: number }>(`/api/v1/audit?${params}`)
    },
    refetchInterval: 10000,
  })

  const entries = data?.entries ?? []
  const totalCount = data?.total ?? 0

  const displayEntries = [...entries].sort((a, b) => {
    if (!sortCol) return 0
    let cmp = 0
    if (sortCol === 'created_at') cmp = new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
    else if (sortCol === 'action') cmp = a.action.localeCompare(b.action)
    else if (sortCol === 'resource_type') cmp = (a.resource_type || '').localeCompare(b.resource_type || '')
    return sortDir === 'asc' ? cmp : -cmp
  })

  return (
    <AppShell>
        <div style={styles.content}>
          <SectionHeader title="Audit Log" subtitle={`${totalCount.toLocaleString()} entr${totalCount !== 1 ? 'ies' : 'y'} total`}>
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
            <input
              style={{ ...styles.filterInput, width: 180 }}
              value={userFilter}
              onChange={(e) => setUserFilter(e.target.value)}
              placeholder="Search user email…"
            />
            <input
              type="date"
              style={{ ...styles.filterInput, width: 150 }}
              value={dateFrom}
              onChange={(e) => setDateFrom(e.target.value)}
              placeholder="From date"
              title="From date"
            />
            <input
              type="date"
              style={{ ...styles.filterInput, width: 150 }}
              value={dateTo}
              onChange={(e) => setDateTo(e.target.value)}
              placeholder="To date"
              title="To date"
            />
          </SectionHeader>

          {error && <ErrorBanner message={`Failed to load audit log: ${(error as Error).message}`} />}
          {isLoading ? (
            <div style={styles.state}>
              <p style={styles.stateText}>Loading audit log…</p>
            </div>
          ) : entries.length === 0 ? (
            <EmptyState
              icon={<span>▦</span>}
              title="No entries found"
              text={(actionFilter || resourceTypeFilter) ? 'No entries match the selected filters.' : 'The audit log is empty.'}
            />
          ) : (
            <StyledTable
              headers={[
                <SortHeader key="ts" label="Timestamp" col="created_at" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} />,
                <SortHeader key="action" label="Action" col="action" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} />,
                <SortHeader key="rt" label="Resource Type" col="resource_type" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} />,
                'Resource',
                'User',
              ]}
              thStyle={{ fontSize: 12, background: 'var(--bg-primary)', letterSpacing: 'normal', borderBottom: '1px solid var(--border)' }}
            >
              {displayEntries.map((entry) => (
                <AuditRow key={entry.id} entry={entry} />
              ))}
            </StyledTable>
          )}

          {!isLoading && totalCount > PAGE_SIZE && (
            <Pagination
              page={page}
              pageSize={PAGE_SIZE}
              total={totalCount}
              onPageChange={setPage}
            />
          )}

          <AuditS3ConfigSection />
        </div>
    </AppShell>
  )
}

function AuditS3ConfigSection() {
  const { data: config, error: configError, isLoading } = useQuery<AuditS3Config>({
    queryKey: ['audit-s3-config'],
    queryFn: () => api.get('/api/v1/audit/s3-config'),
    retry: false,
  })
  const [endpoint, setEndpoint] = useState('')
  const [region, setRegion] = useState('us-east-1')
  const [bucket, setBucket] = useState('')
  const [accessKey, setAccessKey] = useState('')
  const [secretKey, setSecretKey] = useState('')
  const [useRole, setUseRole] = useState(false)
  const [batchSize, setBatchSize] = useState(100)
  const [flushIntervalSecs, setFlushIntervalSecs] = useState(60)
  const [enabled, setEnabled] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [showConfig, setShowConfig] = useState(false)

  useEffect(() => {
    if (config) {
      setEndpoint(config.endpoint ?? '')
      setRegion(config.region ?? 'us-east-1')
      setBucket(config.bucket ?? '')
      setUseRole(config.use_role ?? false)
      setBatchSize(config.batch_size ?? 100)
      setFlushIntervalSecs(config.flush_interval_secs ?? 60)
      setEnabled(config.enabled ?? false)
    }
  }, [config])

  const isForbidden = configError && ((configError as any)?.status === 403 || (configError as Error)?.message?.includes('403'))
  if (isLoading || isForbidden) return null

  async function handleSave() {
    if (!bucket.trim()) { setMessage({ type: 'error', text: 'Bucket is required' }); return }
    setSaving(true)
    setMessage(null)
    try {
      await api.put('/api/v1/audit/s3-config', {
        endpoint, region, bucket,
        access_key: accessKey, secret_key: secretKey,
        use_role: useRole,
        batch_size: batchSize, flush_interval_secs: flushIntervalSecs, enabled,
      })
      setMessage({ type: 'success', text: 'Configuration saved' })
    } catch (e) {
      setMessage({ type: 'error', text: `Save failed: ${(e as Error).message}` })
    } finally {
      setSaving(false)
    }
  }

  async function handleTest() {
    setTesting(true)
    setMessage(null)
    try {
      await api.post('/api/v1/audit/s3-config/test', {})
      setMessage({ type: 'success', text: 'Connection successful' })
    } catch (e) {
      setMessage({ type: 'error', text: `Connection test failed: ${(e as Error).message}` })
    } finally {
      setTesting(false)
    }
  }

  const iStyle: React.CSSProperties = {
    padding: '7px 12px', border: '1px solid var(--border)', borderRadius: 4,
    fontSize: 13, outline: 'none', color: 'var(--text-primary)',
    background: 'var(--bg-input)', width: '100%', boxSizing: 'border-box',
  }
  const lStyle: React.CSSProperties = {
    display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4,
  }
  const rowStyle: React.CSSProperties = { marginBottom: 16 }

  return (
    <div style={{ marginTop: 32, borderTop: '1px solid var(--border)', paddingTop: 24 }}>
      <button
        style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 0, fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}
        onClick={() => setShowConfig(!showConfig)}
      >
        {showConfig ? '−' : '+'} S3 Export Configuration
      </button>

      {showConfig && (
        <div style={{ maxWidth: 600, marginTop: 16 }}>
          <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 20 }}>
            Configure S3 storage for audit log export. Entries are written to both Postgres and S3.
          </p>

          <div style={rowStyle}>
            <label style={lStyle}>S3 Endpoint</label>
            <input style={iStyle} value={endpoint} onChange={e => setEndpoint(e.target.value)}
              placeholder="https://s3.amazonaws.com (leave empty for AWS)" />
          </div>

          <div style={{ display: 'flex', gap: 12, ...rowStyle } as React.CSSProperties}>
            <div style={{ flex: 1 }}>
              <label style={lStyle}>Region</label>
              <input style={iStyle} value={region} onChange={e => setRegion(e.target.value)} placeholder="us-east-1" />
            </div>
            <div style={{ flex: 1 }}>
              <label style={lStyle}>Bucket</label>
              <input style={iStyle} value={bucket} onChange={e => setBucket(e.target.value)} placeholder="my-audit-logs" />
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
            <input type="checkbox" id="org-audit-s3-use-role" checked={useRole} onChange={e => setUseRole(e.target.checked)} />
            <label htmlFor="org-audit-s3-use-role" style={{ fontSize: 13, color: 'var(--text-primary)', cursor: 'pointer' }}>
              Use IAM Role (AWS)
            </label>
          </div>

          {!useRole && (
            <div style={{ display: 'flex', gap: 12, ...rowStyle } as React.CSSProperties}>
              <div style={{ flex: 1 }}>
                <label style={lStyle}>Access Key</label>
                <input style={iStyle} value={accessKey} onChange={e => setAccessKey(e.target.value)} placeholder="AKIA..." type="password" />
              </div>
              <div style={{ flex: 1 }}>
                <label style={lStyle}>Secret Key</label>
                <input style={iStyle} value={secretKey} onChange={e => setSecretKey(e.target.value)} placeholder="leave blank to keep existing" type="password" />
              </div>
            </div>
          )}

          <div style={{ display: 'flex', gap: 12, ...rowStyle } as React.CSSProperties}>
            <div style={{ flex: 1 }}>
              <label style={lStyle}>Batch Size</label>
              <input style={iStyle} type="number" value={batchSize} onChange={e => setBatchSize(parseInt(e.target.value) || 100)} min={1} />
            </div>
            <div style={{ flex: 1 }}>
              <label style={lStyle}>Flush Interval (seconds)</label>
              <input style={iStyle} type="number" value={flushIntervalSecs} onChange={e => setFlushIntervalSecs(parseInt(e.target.value) || 60)} min={1} />
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 20 }}>
            <input type="checkbox" id="org-audit-s3-enabled" checked={enabled} onChange={e => setEnabled(e.target.checked)} />
            <label htmlFor="org-audit-s3-enabled" style={{ fontSize: 13, color: 'var(--text-primary)', cursor: 'pointer' }}>Enabled</label>
          </div>

          {message && (
            <div style={{
              padding: '8px 12px', borderRadius: 4, fontSize: 13, marginBottom: 16,
              background: message.type === 'success' ? 'var(--accent-light)' : 'var(--error-light)',
              color: message.type === 'success' ? 'var(--accent)' : 'var(--error)',
            }}>
              {message.text}
            </div>
          )}

          <div style={{ display: 'flex', gap: 8 }}>
            <button style={{
              padding: '7px 16px', background: 'var(--accent)', color: '#fff',
              border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer',
            }} onClick={handleSave} disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </button>
            <button style={{
              padding: '7px 16px', background: 'none', color: 'var(--text-secondary)',
              border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer',
            }} onClick={handleTest} disabled={testing}>
              {testing ? 'Testing…' : 'Test Connection'}
            </button>
          </div>
        </div>
      )}
    </div>
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
      return <span>{parent} <span style={styles.resourceSub} title={resource_id} onClick={handleCopy} className="cursor-pointer">› {id}</span></span>
    }
    if (parent) return <span>{parent}</span>
    return <span style={styles.mono}>{id || '—'}</span>
  }

  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
      {resource_name && <span>{resource_name}</span>}
      {resource_id && (
        <span
          style={{ ...(resource_name ? styles.resourceSub : styles.mono), cursor: 'pointer' }}
          title={`Click to copy: ${resource_id}`}
          onClick={handleCopy}
        >
          {truncateId(resource_id)}
        </span>
      )}
    </span>
  )
}

function CellExecuteMeta({ metadata }: { metadata: Record<string, unknown> }) {
  const query = typeof metadata.query === 'string' ? metadata.query : ''
  const rowCount = metadata.row_count != null ? String(metadata.row_count) : null
  const durationMs = metadata.duration_ms != null ? String(metadata.duration_ms) : null
  const queryPreview = query.length > 200 ? query.slice(0, 200) + '…' : query

  return (
    <div style={styles.cellMeta}>
      {queryPreview && (
        <div style={styles.cellMetaQuery} title={query}>
          <code>{queryPreview}</code>
        </div>
      )}
      <div style={styles.cellMetaStats}>
        {rowCount && <span>{rowCount} rows</span>}
        {rowCount && durationMs && <span> · </span>}
        {durationMs && <span>{durationMs}ms</span>}
      </div>
    </div>
  )
}

function AuditRow({ entry }: { entry: AuditEntry }) {
  const ts = new Date(entry.created_at)
  const dateStr = ts.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
  const timeStr = ts.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short' })
  const isCellExecute = entry.action === 'cell.execute' && entry.metadata

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
        {isCellExecute && <CellExecuteMeta metadata={entry.metadata!} />}
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

function SortHeader({ label, col, sortCol, sortDir, onSort }: { label: string; col: 'created_at' | 'action' | 'resource_type'; sortCol: 'created_at' | 'action' | 'resource_type' | ''; sortDir: 'asc' | 'desc'; onSort: (col: 'created_at' | 'action' | 'resource_type') => void }) {
  const active = sortCol === col
  return (
    <button
      type="button"
      onClick={() => onSort(col)}
      style={{
        background: 'none',
        border: 'none',
        cursor: 'pointer',
        padding: 0,
        font: 'inherit',
        color: 'inherit',
        letterSpacing: 'inherit',
        textTransform: 'inherit',
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        fontWeight: active ? 700 : 600,
      }}
    >
      {label}
      {active && (
        sortDir === 'asc' ? <ChevronUp size={12} /> : <ChevronDown size={12} />
      )}
    </button>
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
  cellMeta: {
    marginTop: 4,
    fontSize: 11,
    lineHeight: 1.4,
  },
  cellMetaQuery: {
    color: 'var(--text-secondary)',
    maxWidth: 400,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
  },
  cellMetaStats: {
    color: 'var(--text-muted)',
    fontFamily: 'monospace',
    fontSize: 10,
    marginTop: 2,
  },
}
