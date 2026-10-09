import { useState, useEffect, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Connector, ConnectorCloudState } from '../types'
import { AppShell } from '../components/AppShell'
import { Check, X, Star, Database, Pencil, ShieldCheck, Link2, Unlink, Trash2, Zap } from 'lucide-react'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { StatusBadge } from '../components/StatusBadge'
import { SectionHeader } from '../components/SectionHeader'
import { PermissionsPanel } from '../components/PermissionsPanel'
import { EmptyState } from '../components/EmptyState'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { Modal } from '../components/Modal'
import { FormModal } from '../components/FormModal'
import { RowAction, RowActionsBreak, RowActionsCell, RowActionsHeader } from '../components/RowActions'
import { useAuth } from '../hooks/useAuth'
import { useWarehouseTablePermissions } from '../hooks/useWarehouseTablePermissions'
import { listWarehouses, setConnectorWarehouse } from '../api/warehouses'
import { formatRelativeTime } from '../utils/formatRelativeTime'
import {
  cloudStateView,
  connectorIdleTimeoutMinutes,
  formatRelativeAgo,
  inferIdleState,
  parseIdleTimeoutMinutes,
} from '../utils/cloudState'

type ConnectorType = 'postgres' | 'clickhouse' | 'opensearch' | 'databricks'
type DatabricksAuthType = 'pat' | 'oauth_m2m'

interface ConnectorForm {
  name: string
  type: ConnectorType
  host: string
  port: string
  database: string
  user: string
  password: string
  ssl_mode: string
  use_tls: boolean
  http_path: string
  auth_type: DatabricksAuthType
  token: string
  client_id: string
  client_secret: string
  catalog: string
  schema: string
  /** Auth type loaded from the stored connector; '' when creating. Used to
   * allow blank secrets only when the auth type is unchanged. */
  stored_auth_type: DatabricksAuthType | ''
  is_default: boolean
  timeout_seconds: string
  table_allowlist: string
  table_denylist: string
  idle_timeout_minutes: string
  cloud_org_id: string
  cloud_service_id: string
  cloud_key_id: string
  cloud_key_secret: string
}

const defaultForm = (): ConnectorForm => ({
  name: '', type: 'postgres', host: 'localhost', port: '5432',
  database: '', user: '', password: '', ssl_mode: 'disable',
  use_tls: false, http_path: '', auth_type: 'pat', token: '',
  client_id: '', client_secret: '', catalog: '', schema: '', stored_auth_type: '',
  is_default: false, timeout_seconds: '0',
  table_allowlist: '', table_denylist: '',
  idle_timeout_minutes: '', cloud_org_id: '', cloud_service_id: '', cloud_key_id: '', cloud_key_secret: '',
})

/** Builds the per-type config object for create (forUpdate=false) and edit
 * (forUpdate=true, secrets omitted when blank to keep the stored value). */
function buildConnectorConfig(f: ConnectorForm, forUpdate: boolean): Record<string, unknown> {
  if (f.type === 'databricks') {
    const cfg: Record<string, unknown> = {
      host: f.host,
      http_path: f.http_path,
      auth_type: f.auth_type,
      catalog: f.catalog,
      schema: f.schema,
    }
    if (f.auth_type === 'pat') {
      if (!forUpdate || f.token !== '') cfg.token = f.token
    } else {
      cfg.client_id = f.client_id
      if (!forUpdate || f.client_secret !== '') cfg.client_secret = f.client_secret
    }
    return cfg
  }
  const cfg: Record<string, unknown> = {
    host: f.host,
    port: parseInt(f.port),
    database: f.database,
    user: f.user,
    ssl_mode: f.ssl_mode,
    ...(f.type === 'opensearch' ? { use_tls: f.use_tls } : {}),
  }
  if (f.type === 'clickhouse') {
    // The manual idle threshold always round-trips (empty = the 15m default).
    cfg.idle_timeout_minutes = parseIdleTimeoutMinutes(f.idle_timeout_minutes)
    // Non-secret Cloud API fields round-trip verbatim (blank clears access);
    // the secret is omitted on edit when blank so the server keeps the stored one.
    cfg.cloud_org_id = f.cloud_org_id.trim()
    cfg.cloud_service_id = f.cloud_service_id.trim()
    cfg.cloud_key_id = f.cloud_key_id.trim()
    if (!forUpdate || f.cloud_key_secret !== '') cfg.cloud_key_secret = f.cloud_key_secret
  }
  if (!forUpdate || f.password !== '') cfg.password = f.password
  return cfg
}

/** Whether the type-specific config fields are filled. Credentials may stay
 * blank on edit (the server keeps the stored secret). */
function connectorConfigComplete(f: ConnectorForm, forUpdate: boolean): boolean {
  if (!f.host) return false
  if (f.type === 'postgres' && !f.database) return false
  if (f.type === 'databricks') {
    if (!f.http_path) return false
    // A blank secret is only acceptable when the auth type is unchanged from
    // the stored connector (the server keeps the stored secret in that case).
    const sameStoredAuth = forUpdate && f.stored_auth_type === f.auth_type
    if (f.auth_type === 'pat') return f.token !== '' || sameStoredAuth
    return f.client_id !== '' && (f.client_secret !== '' || sameStoredAuth)
  }
  return true
}

function canSubmitConnector(f: ConnectorForm, forUpdate: boolean): boolean {
  return f.name !== '' && connectorConfigComplete(f, forUpdate)
}

/** First missing required field, for the submit buttons' title attribute. */
function connectorFormMissingField(f: ConnectorForm, forUpdate: boolean): string | undefined {
  if (!f.host) return 'Host is required'
  if (f.type === 'postgres' && !f.database) return 'Database is required'
  if (f.type === 'databricks') {
    if (!f.http_path) return 'HTTP Path is required'
    const sameStoredAuth = forUpdate && f.stored_auth_type === f.auth_type
    if (f.auth_type === 'pat') return f.token !== '' || sameStoredAuth ? undefined : 'Token is required'
    if (!f.client_id) return 'Client ID is required'
    return f.client_secret !== '' || sameStoredAuth ? undefined : 'Client Secret is required'
  }
  return undefined
}

/** Maps a stored connector onto the edit form. Secrets stay blank: the server
 * keeps the stored value when the form submits an empty secret. */
function formFromConnector(c: Connector): ConnectorForm {
  return {
    name: c.name,
    type: c.type as ConnectorType,
    host: c.config?.host ?? '',
    port: String(c.config?.port ?? 5432),
    database: c.config?.database ?? '',
    user: c.config?.user ?? '',
    password: '',
    ssl_mode: c.config?.ssl_mode ?? 'disable',
    use_tls: c.config?.use_tls ?? false,
    http_path: c.config?.http_path ?? '',
    auth_type: c.config?.auth_type === 'oauth_m2m' ? 'oauth_m2m' : 'pat',
    stored_auth_type: c.type === 'databricks'
      ? (c.config?.auth_type === 'oauth_m2m' ? 'oauth_m2m' : 'pat')
      : '',
    token: '',
    client_id: c.config?.client_id ?? '',
    client_secret: '',
    catalog: c.config?.catalog ?? '',
    schema: c.config?.schema ?? '',
    is_default: c.is_default ?? false,
    timeout_seconds: String(c.timeout_seconds ?? 0),
    table_allowlist: (c.table_allowlist ?? []).join('\n'),
    table_denylist: (c.table_denylist ?? []).join('\n'),
    idle_timeout_minutes: c.config?.idle_timeout_minutes != null ? String(c.config.idle_timeout_minutes) : '',
    cloud_org_id: c.config?.cloud_org_id ?? '',
    cloud_service_id: c.config?.cloud_service_id ?? '',
    cloud_key_id: c.config?.cloud_key_id ?? '',
    cloud_key_secret: '',
  }
}

/** Derives the persisted health badge from the connector's outcome timeline
 * (D6): a failure newer than the last success wins; otherwise a success means
 * Connected; otherwise the connector has never been exercised. */
function connectorHealth(c: Connector): { status: 'success' | 'error' | 'neutral'; label: string; title?: string } {
  const failed = !!c.last_failure_at && (!c.last_success_at || new Date(c.last_failure_at) > new Date(c.last_success_at))
  if (failed) {
    const when = formatRelativeTime(c.last_failure_at)
    return { status: 'error', label: when ? `Failed · ${when}` : 'Failed', title: c.last_error || undefined }
  }
  if (c.last_success_at) {
    const when = formatRelativeTime(c.last_success_at)
    return { status: 'success', label: when ? `Connected · used ${when}` : 'Connected' }
  }
  return { status: 'neutral', label: 'Never used — click Test' }
}

function DatabricksFields({ form, setForm, isEdit }: {
  form: ConnectorForm
  setForm: React.Dispatch<React.SetStateAction<ConnectorForm>>
  isEdit?: boolean
}) {
  const set = (field: keyof ConnectorForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))
  return (
    <>
      <label style={styles.label}>HTTP Path
        <input style={styles.input} value={form.http_path} onChange={set('http_path')} placeholder="/sql/1.0/warehouses/…" />
      </label>
      <label style={styles.label}>Auth Type
        <select style={styles.input} value={form.auth_type} onChange={set('auth_type')}>
          <option value="pat">Personal Access Token</option>
          <option value="oauth_m2m">OAuth (Service Principal)</option>
        </select>
      </label>
      {form.auth_type === 'pat' ? (
        <label style={styles.label}>Token{isEdit ? ' (leave blank to keep current)' : ''}
          <input style={styles.input} type="password" value={form.token} onChange={set('token')} />
        </label>
      ) : (
        <>
          <label style={styles.label}>Client ID
            <input style={styles.input} value={form.client_id} onChange={set('client_id')} />
          </label>
          <label style={styles.label}>Client Secret{isEdit ? ' (leave blank to keep current)' : ''}
            <input style={styles.input} type="password" value={form.client_secret} onChange={set('client_secret')} />
          </label>
        </>
      )}
      <label style={styles.label}>Catalog
        <input style={styles.input} value={form.catalog} onChange={set('catalog')} placeholder="main (optional)" />
      </label>
      <label style={styles.label}>Schema
        <input style={styles.input} value={form.schema} onChange={set('schema')} placeholder="default (optional)" />
      </label>
    </>
  )
}

function ClickHouseCloudFields({ form, setForm, isEdit }: {
  form: ConnectorForm
  setForm: React.Dispatch<React.SetStateAction<ConnectorForm>>
  isEdit?: boolean
}) {
  const set = (field: keyof ConnectorForm) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))
  return (
    <>
      <label style={styles.label}>Idle timeout (minutes)
        <input
          style={styles.input}
          type="number"
          min="1"
          value={form.idle_timeout_minutes}
          onChange={set('idle_timeout_minutes')}
          placeholder="15"
        />
      </label>
      <details style={{ gridColumn: '1 / -1', fontSize: 13 }}>
        <summary style={{ cursor: 'pointer', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>
          ClickHouse Cloud API (optional)
        </summary>
        <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '8px 0', lineHeight: 1.5 }}>
          Add an API key to show the service's exact state (running, idle, stopped). Aether reads
          the control plane, which never wakes an idle service. Without a key, idle state is
          inferred from recorded query activity using the timeout above.
        </p>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
          <label style={styles.label}>Organization ID
            <input style={styles.input} value={form.cloud_org_id} onChange={set('cloud_org_id')} />
          </label>
          <label style={styles.label}>Service ID
            <input style={styles.input} value={form.cloud_service_id} onChange={set('cloud_service_id')} />
          </label>
          <label style={styles.label}>Key ID
            <input style={styles.input} value={form.cloud_key_id} onChange={set('cloud_key_id')} />
          </label>
          <label style={styles.label}>
            Key Secret{isEdit ? ' (leave blank to keep current)' : ''}
            <input style={styles.input} type="password" value={form.cloud_key_secret} onChange={set('cloud_key_secret')} />
          </label>
        </div>
      </details>
    </>
  )
}

/** Second status line for ClickHouse connectors: exact Cloud control-plane
 * state when credentials are configured, probabilistic activity inference
 * otherwise. Control-plane reads never wake the service; the query polls one
 * connector at a time while the page stays open. */
function ClickHouseCloudStatus({ connector }: { connector: Connector }) {
  const configuredHint = Boolean(
    connector.config?.cloud_org_id &&
    connector.config?.cloud_service_id &&
    connector.config?.cloud_key_id &&
    connector.config?.cloud_key_secret,
  )
  // Query only when the answer can change what the row renders: the exact
  // state requires credentials, and inference is computed locally from the
  // connector list data. Rows without credentials must make no request — the
  // server would deterministically answer {"configured": false} — and a
  // transient fetch error can never paint a misleading chip on them.
  const enabled = configuredHint

  const { data, isError } = useQuery({
    queryKey: ['connector-cloud-state', connector.id],
    queryFn: () => api.get<ConnectorCloudState>(`/api/v1/connectors/${connector.id}/cloud-state`),
    enabled,
    staleTime: 30_000,
    refetchInterval: 60_000,
    retry: false,
  })

  const lineStyle: React.CSSProperties = {
    fontSize: 11,
    color: 'var(--text-muted)',
    marginTop: 4,
    fontStyle: 'italic',
  }

  if (data?.configured && data.error) {
    return (
      <div style={lineStyle}>
        <StatusBadge status="neutral" label="Cloud state unavailable" title={data.error} />
      </div>
    )
  }
  if (isError) {
    return (
      <div style={lineStyle}>
        <StatusBadge status="neutral" label="Cloud state unavailable" title="Could not load Cloud state from Aether" />
      </div>
    )
  }
  if (data?.configured && data.state) {
    const view = cloudStateView(data.state)
    const scaling = data.idle_scaling ? 'Idle scaling on' : 'Idle scaling off'
    const timeout = data.idle_timeout_minutes ? `, timeout ${data.idle_timeout_minutes}m` : ''
    const checked = data.checked_at
      ? ` · checked ${formatRelativeAgo(Date.now() - new Date(data.checked_at).getTime())}`
      : ''
    return (
      <div style={lineStyle}>
        <StatusBadge status={view.status} label={view.label} title={`${scaling}${timeout}${checked}`} />
      </div>
    )
  }
  if (!data && configuredHint) {
    // Credentials exist but the first response has not arrived: don't flash an
    // inference line the exact state is about to replace.
    return null
  }

  const inference = inferIdleState({
    host: connector.config?.host,
    lastSuccessAt: connector.last_success_at,
    idleTimeoutMinutes: connectorIdleTimeoutMinutes(connector),
  })
  if (!inference) return null
  if (inference.kind === 'likely_idle') {
    return (
      <div style={lineStyle}>
        {`Likely idle — last activity ${formatRelativeAgo(inference.lastActivityAgeMs ?? 0)} (idle timeout ${inference.idleTimeoutMinutes}m)`}
      </div>
    )
  }
  if (inference.kind === 'active') {
    return <div style={lineStyle}>Active recently</div>
  }
  return <div style={lineStyle}>Idle state unknown</div>
}

export function ConnectorsPage() {
  useEffect(() => { document.title = "Connectors — Aether Notebooks" }, [])
  const qc = useQueryClient()
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const tablePermissionsEnabled = useWarehouseTablePermissions()
  const [searchParams, setSearchParams] = useSearchParams()
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<ConnectorForm>(defaultForm())
  const [form, setForm] = useState<ConnectorForm>(defaultForm())
  const [testingIds, setTestingIds] = useState<Record<string, boolean>>({})
  const [createError, setCreateError] = useState<string | null>(null)
  const [editError, setEditError] = useState<string | null>(null)
  const [formTest, setFormTest] = useState<{ ok: boolean; error?: string } | null>(null)
  const [formTesting, setFormTesting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [permissionsTarget, setPermissionsTarget] = useState<{ type: 'connector'; id: string; name: string } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Connector | null>(null)
  const [linkTarget, setLinkTarget] = useState<Connector | null>(null)
  const [linkWarehouseId, setLinkWarehouseId] = useState('')
  const [linkError, setLinkError] = useState<string | null>(null)
  const [unlinkTarget, setUnlinkTarget] = useState<Connector | null>(null)
  const createNameRef = useRef<HTMLInputElement>(null)
  const editNameRef = useRef<HTMLInputElement>(null)

  const openEdit = (c: Connector) => {
    setEditing(c.id)
    setEditForm(formFromConnector(c))
    setEditError(null)
  }

  const closeCreate = () => {
    setCreating(false)
    setForm(defaultForm())
    setFormTest(null)
    setCreateError(null)
  }

  const closeEdit = () => {
    setEditing(null)
    setEditForm(defaultForm())
    setEditError(null)
  }

  const { data: connectors = [], isLoading } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })

  const { data: warehouses = [] } = useQuery({
    queryKey: ['warehouses'],
    queryFn: listWarehouses,
    enabled: isAdmin,
  })

  const setWarehouseLink = useMutation({
    mutationFn: ({ connectorId, warehouseId }: { connectorId: string; warehouseId: string | null }) =>
      setConnectorWarehouse(connectorId, warehouseId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['connectors'] })
      qc.invalidateQueries({ queryKey: ['warehouses'] })
      qc.invalidateQueries({ queryKey: ['warehouse'] })
      setLinkTarget(null)
      setUnlinkTarget(null)
      setLinkError(null)
    },
    onError: (err: Error) => setLinkError(err.message),
  })

  useEffect(() => {
    const editId = searchParams.get('edit')
    if (editId && connectors.length > 0) {
      const c = connectors.find(x => x.id === editId)
      if (c) {
        setEditing(c.id)
        setEditForm(formFromConnector(c))
        setSearchParams({})
      }
    }
  }, [searchParams, connectors, setSearchParams])

  // Deep link from warehouse grant warnings: /connectors?permissions=<id>
  // opens the connector's ACL panel directly.
  useEffect(() => {
    const permissionsId = searchParams.get('permissions')
    if (permissionsId && connectors.length > 0) {
      const c = connectors.find(x => x.id === permissionsId)
      if (c) {
        setPermissionsTarget({ type: 'connector', id: c.id, name: c.name })
        setSearchParams({})
      }
    }
  }, [searchParams, connectors, setSearchParams])

  const updateConnector = useMutation({
    mutationFn: (id: string) => api.put<Connector>(`/api/v1/connectors/${id}`, {
      name: editForm.name,
      timeout_seconds: parseInt(editForm.timeout_seconds) || 0,
      config: buildConnectorConfig(editForm, true),
      ...(editForm.is_default ? { is_default: true } : {}),
      table_allowlist: editForm.table_allowlist.split('\n').filter(s => s.trim()),
      table_denylist: editForm.table_denylist.split('\n').filter(s => s.trim()),
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['connectors'] })
      qc.invalidateQueries({ queryKey: ['connector-cloud-state'] })
      closeEdit()
    },
    onError: (e: Error) => setEditError(e.message),
  })

  const createConnector = useMutation({
    mutationFn: () => api.post<Connector>('/api/v1/connectors', {
      name: form.name,
      type: form.type,
      is_default: form.is_default,
      timeout_seconds: parseInt(form.timeout_seconds) || 0,
      config: buildConnectorConfig(form, false),
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['connectors'] })
      closeCreate()
    },
    onError: (err: Error) => setCreateError(err.message),
  })

  const deleteConnector = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/connectors/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['connectors'] }),
    onError: (err: Error) => setDeleteError(err.message),
  })

  const setDefault = useMutation({
    mutationFn: (id: string) => api.put(`/api/v1/connectors/${id}/default`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['connectors'] }),
  })

  const testConnector = async (id: string) => {
    setTestingIds((prev) => ({ ...prev, [id]: true }))
    try {
      await api.post<{ ok: boolean; error?: string }>(`/api/v1/connectors/${id}/test`, {})
    } catch {
      // The outcome is persisted server-side; the refetch below surfaces it.
      // A network failure leaves the last known persisted state in place.
    } finally {
      await qc.invalidateQueries({ queryKey: ['connectors'] })
      setTestingIds((prev) => { const n = { ...prev }; delete n[id]; return n })
    }
  }

  const testFormConnection = async () => {
    setFormTesting(true)
    setFormTest(null)
    try {
      const result = await api.post<{ ok: boolean; error?: string }>('/api/v1/connectors/test', {
        type: form.type,
        config: buildConnectorConfig(form, false),
      })
      setFormTest(result)
    } catch {
      setFormTest({ ok: false, error: 'Request failed' })
    } finally {
      setFormTesting(false)
    }
  }

  const setField = (field: keyof ConnectorForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))

  const editingConnector = connectors.find((c) => c.id === editing)

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader title="Connectors" subtitle={connectors.length > 0 ? `${connectors.length} connector${connectors.length !== 1 ? 's' : ''}` : ''}>
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Connector</button>
        </SectionHeader>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: -16, marginBottom: 24 }}>
          Connect to your databases (PostgreSQL, ClickHouse, OpenSearch, Databricks) to query data from notebooks.
        </p>
        {creating && (
          <FormModal
            title="New Connector"
            onClose={closeCreate}
            initialFocusRef={createNameRef}
            error={createError}
            footerExtra={
              <>
                <button
                  type="button"
                  className="form-modal-btn"
                  onClick={testFormConnection}
                  disabled={!connectorConfigComplete(form, false) || formTesting}
                  title={connectorFormMissingField(form, false)}
                >
                  {formTesting ? 'Testing…' : 'Test Connection'}
                </button>
                {formTest && (
                  <StatusBadge
                    status={formTest.ok ? 'success' : 'error'}
                    label={formTest.ok ? 'Connected' : (formTest.error ?? 'Failed')}
                    icon={formTest.ok ? <Check size={12} /> : <X size={12} />}
                  />
                )}
              </>
            }
            submitLabel="Create"
            pendingLabel="Creating…"
            pending={createConnector.isPending}
            submitDisabled={!canSubmitConnector(form, false)}
            submitTitle={!form.name ? 'Name is required' : connectorFormMissingField(form, false)}
            onSubmit={() => createConnector.mutate()}
          >
              <label style={styles.label}>Name
                <input ref={createNameRef} style={styles.input} value={form.name} onChange={setField('name')} placeholder="My Postgres" />
              </label>
              <label style={styles.label}>Type
                <select style={styles.input} value={form.type} onChange={(e) => setForm((f) => ({
                  ...f, type: e.target.value as ConnectorType,
                  port: e.target.value === 'clickhouse' ? '9000' : e.target.value === 'opensearch' ? '9200' : '5432',
                }))}>
                  <option value="postgres">PostgreSQL</option>
                  <option value="clickhouse">ClickHouse</option>
                  <option value="opensearch">OpenSearch</option>
                  <option value="databricks">Databricks</option>
                </select>
              </label>
              <label style={styles.label}>Host
                <input style={styles.input} value={form.host} onChange={setField('host')} />
              </label>
              {form.type !== 'databricks' && (
                <>
                  <label style={styles.label}>Port
                    <input style={styles.input} type="text" value={form.port} onChange={setField('port')} />
                  </label>
                  {(form.type === 'postgres' || form.type === 'clickhouse') && (
                    <label style={styles.label}>Database
                      <input style={styles.input} value={form.database} onChange={setField('database')} />
                    </label>
                  )}
                  <label style={styles.label}>User
                    <input style={styles.input} value={form.user} onChange={setField('user')} />
                  </label>
                  <label style={styles.label}>Password
                    <input style={styles.input} type="password" value={form.password} onChange={setField('password')} />
                  </label>
                  {form.type === 'opensearch' && (
                    <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
                      <input type="checkbox" checked={form.use_tls}
                        onChange={e => setForm(f => ({ ...f, use_tls: e.target.checked }))} />
                      Use TLS (HTTPS)
                    </label>
                  )}
                  {(form.type === 'postgres' || form.type === 'clickhouse') && (
                    <label style={styles.label}>SSL Mode
                      <select style={styles.input} value={form.ssl_mode} onChange={setField('ssl_mode')}>
                        <option value="disable">disable</option>
                        <option value="require">require</option>
                        <option value="verify-full">verify-full</option>
                      </select>
                    </label>
                  )}
                </>
              )}
              {form.type === 'databricks' && <DatabricksFields form={form} setForm={setForm} />}
              {form.type === 'clickhouse' && <ClickHouseCloudFields form={form} setForm={setForm} />}
              <label style={styles.label}>Query Timeout (s)
                <input style={styles.input} type="text" value={form.timeout_seconds}
                  onChange={(e) => setForm(f => ({ ...f, timeout_seconds: e.target.value }))} placeholder="0 = unlimited" />
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)', gridColumn: '1 / -1' }}>
                <input type="checkbox" checked={form.is_default ?? false}
                  onChange={e => setForm(f => ({ ...f, is_default: e.target.checked }))} />
                Set as default connector for new notebooks
              </label>
          </FormModal>
        )}

        {editing && (
          <FormModal
            title={`Edit "${editingConnector?.name ?? 'connector'}"`}
            onClose={closeEdit}
            initialFocusRef={editNameRef}
            error={editError}
            submitLabel="Save"
            pendingLabel="Saving…"
            pending={updateConnector.isPending}
            submitDisabled={!canSubmitConnector(editForm, true)}
            submitTitle={!editForm.name ? 'Name is required' : connectorFormMissingField(editForm, true)}
            onSubmit={() => updateConnector.mutate(editing!)}
          >
              <label style={styles.label}>Name
                <input ref={editNameRef} style={styles.input} value={editForm.name} onChange={(e) => setEditForm(f => ({ ...f, name: e.target.value }))} />
              </label>
              <label style={styles.label}>Type
                <select style={styles.input} value={editForm.type} onChange={(e) => setEditForm((f) => ({
                  ...f, type: e.target.value as ConnectorType,
                  port: e.target.value === 'clickhouse' ? '9000' : e.target.value === 'opensearch' ? '9200' : '5432',
                }))}>
                  <option value="postgres">PostgreSQL</option>
                  <option value="clickhouse">ClickHouse</option>
                  <option value="opensearch">OpenSearch</option>
                  <option value="databricks">Databricks</option>
                </select>
              </label>
              <label style={styles.label}>Host
                <input style={styles.input} value={editForm.host} onChange={(e) => setEditForm(f => ({ ...f, host: e.target.value }))} />
              </label>
              {editForm.type !== 'databricks' && (
                <>
                  <label style={styles.label}>Port
                    <input style={styles.input} type="text" value={editForm.port} onChange={(e) => setEditForm(f => ({ ...f, port: e.target.value }))} />
                  </label>
                  {(editForm.type === 'postgres' || editForm.type === 'clickhouse') && (
                    <label style={styles.label}>Database
                      <input style={styles.input} value={editForm.database} onChange={(e) => setEditForm(f => ({ ...f, database: e.target.value }))} />
                    </label>
                  )}
                  <label style={styles.label}>User
                    <input style={styles.input} value={editForm.user} onChange={(e) => setEditForm(f => ({ ...f, user: e.target.value }))} />
                  </label>
                  <label style={styles.label}>Password <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>(leave blank to keep current)</span>
                    <input style={styles.input} type="password" value={editForm.password} onChange={(e) => setEditForm(f => ({ ...f, password: e.target.value }))} />
                  </label>
                  {editForm.type === 'opensearch' && (
                    <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
                      <input type="checkbox" checked={editForm.use_tls}
                        onChange={e => setEditForm(f => ({ ...f, use_tls: e.target.checked }))} />
                      Use TLS (HTTPS)
                    </label>
                  )}
                  {(editForm.type === 'postgres' || editForm.type === 'clickhouse') && (
                    <label style={styles.label}>SSL Mode
                      <select style={styles.input} value={editForm.ssl_mode} onChange={(e) => setEditForm(f => ({ ...f, ssl_mode: e.target.value }))}>
                        <option value="disable">disable</option>
                        <option value="require">require</option>
                        <option value="verify-full">verify-full</option>
                      </select>
                    </label>
                  )}
                </>
              )}
              {editForm.type === 'databricks' && <DatabricksFields form={editForm} setForm={setEditForm} isEdit />}
              {editForm.type === 'clickhouse' && <ClickHouseCloudFields form={editForm} setForm={setEditForm} isEdit />}
              <label style={styles.label}>Query Timeout (s)
                <input style={styles.input} type="text" value={editForm.timeout_seconds}
                  onChange={(e) => setEditForm(f => ({ ...f, timeout_seconds: e.target.value }))} placeholder="0 = unlimited" />
              </label>
              <label style={{ ...styles.label, gridColumn: '1 / -1' }}>
                Table Allowlist (regex patterns, one per line, empty = all tables)
                <textarea
                  style={{ ...styles.input, minHeight: 60, fontFamily: 'var(--font-mono)', fontSize: 12, resize: 'vertical' }}
                  value={editForm.table_allowlist}
                  onChange={(e) => setEditForm(f => ({ ...f, table_allowlist: e.target.value }))}
                  placeholder="e.g.,^public\\..*\\n^analytics\\..*"
                />
              </label>
              <label style={{ ...styles.label, gridColumn: '1 / -1' }}>
                Table Denylist (regex patterns, one per line)
                <textarea
                  style={{ ...styles.input, minHeight: 60, fontFamily: 'var(--font-mono)', fontSize: 12, resize: 'vertical' }}
                  value={editForm.table_denylist}
                  onChange={(e) => setEditForm(f => ({ ...f, table_denylist: e.target.value }))}
                  placeholder="e.g.,.*_src$\\n^temp\\..*"
                />
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)', gridColumn: '1 / -1' }}>
                <input type="checkbox" checked={editForm.is_default ?? false}
                  onChange={e => setEditForm(f => ({ ...f, is_default: e.target.checked }))} />
                Set as default connector for new notebooks
              </label>
          </FormModal>
        )}

        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}
        {linkError && !linkTarget && <p style={{ color: 'var(--error)', fontSize: 12 }}>{linkError}</p>}
        {connectors.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Database size={28} />}
            title="No connectors yet"
            text="Add a connector to link your databases and start querying."
            action={{ label: '+ New Connector', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable
            headers={['Name', 'Type', 'Host', 'Database', 'Access', 'Status', <RowActionsHeader />]}
            headerClassNames={[undefined, undefined, undefined, undefined, undefined, undefined, 'row-actions-header']}
          >
            {connectors.map((c) => {
              const health = connectorHealth(c)
              return (
                <tr key={c.id} style={rowStyle}>
                  <td style={cellStyle}>
                    <strong>{c.name}</strong>
                    {c.is_default && (
                      <span style={{
                        fontSize: 11,
                        background: 'var(--accent-light)',
                        border: '1px solid var(--accent)',
                        borderRadius: 10,
                        padding: '2px 8px',
                        color: 'var(--accent)',
                        fontWeight: 600,
                        marginLeft: 8,
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 3,
                      }}>
                        <Star size={10} fill="var(--accent)" />
                        Default
                      </span>
                    )}
                    {c.is_provisioner && (
                      <span style={{
                        fontSize: 11,
                        background: 'var(--bg-input)',
                        border: '1px solid var(--border)',
                        borderRadius: 10,
                        padding: '2px 8px',
                        color: 'var(--text-secondary)',
                        fontWeight: 600,
                        marginLeft: 8,
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 3,
                      }}>
                        Provisioner
                      </span>
                    )}
                  </td>
                  <td style={cellStyle}>
                    <code className={'conn-type-chip conn-type-chip--' + c.type}>{c.type}</code>
                  </td>
                  <td style={{
                    ...cellStyle,
                    fontFamily: 'var(--font-mono)',
                    fontSize: 12,
                    // Hosts are unbreakable strings: allow wrapping so the table
                    // can shrink, but keep common hostnames on one line.
                    overflowWrap: 'anywhere',
                    minWidth: 180,
                  }}>
                    {c.config?.host ?? '—'}
                  </td>
                  <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                    {c.config?.database || '—'}
                  </td>
                  <td style={cellStyle}>
                    {c.warehouse_id ? (
                      <span
                        style={styles.managedBadge}
                        title={tablePermissionsEnabled
                          ? `Managed by warehouse "${
                              warehouses.find((w) => w.id === c.warehouse_id)?.name ?? c.warehouse_id
                            }" — ClickHouse table grants are enforced`
                          : `Linked to warehouse "${
                              warehouses.find((w) => w.id === c.warehouse_id)?.name ?? c.warehouse_id
                            }" — ClickHouse table permissions are disabled, so queries use the connector's stored credential`}
                      >
                        {tablePermissionsEnabled ? 'Managed — table grants enforced' : 'Managed — table grants off'}
                      </span>
                    ) : (
                      <span style={styles.sharedBadge}>
                        Shared credential — not table-scoped
                      </span>
                    )}
                  </td>
                  <td style={cellStyle}>
                    {testingIds[c.id] ? (
                      <StatusBadge status="neutral" label="Testing…" />
                    ) : health.status === 'neutral' ? (
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>
                        {health.label}
                      </span>
                    ) : (
                      <StatusBadge
                        status={health.status}
                        label={health.label}
                        title={health.title}
                        icon={health.status === 'success' ? <Check size={12} /> : <X size={12} />}
                      />
                    )}
                    {c.type === 'clickhouse' && <ClickHouseCloudStatus connector={c} />}
                  </td>
                  <RowActionsCell>
                    <RowAction label="Test connection" icon={<Zap size={13} />} spinning={!!testingIds[c.id]} disabled={!!testingIds[c.id]} onClick={() => testConnector(c.id)} />
                    <RowAction label="Edit connector" icon={<Pencil size={13} />} accent onClick={() => openEdit(c)} />
                    <RowActionsBreak />
                    <RowAction label="Permissions" icon={<ShieldCheck size={13} />} onClick={() => setPermissionsTarget({ type: 'connector', id: c.id, name: c.name })} />
                    {isAdmin && c.type === 'clickhouse' && (
                      c.warehouse_id ? (
                        <RowAction label="Unlink from warehouse" icon={<Unlink size={13} />} onClick={() => setUnlinkTarget(c)} />
                      ) : (
                        <RowAction label="Link to warehouse" icon={<Link2 size={13} />} onClick={() => { setLinkTarget(c); setLinkWarehouseId(''); setLinkError(null) }} />
                      )
                    )}
                    <RowActionsBreak />
                    {!c.is_default && (
                      <RowAction label="Set as default connector" title="Set as default connector for new notebooks" icon={<Star size={13} />} onClick={() => setDefault.mutate(c.id)} />
                    )}
                    <RowAction label="Delete connector" icon={<Trash2 size={13} />} danger onClick={() => setDeleteTarget(c)} />
                  </RowActionsCell>
                </tr>
              )
            })}
          </StyledTable>
        )}
        {permissionsTarget && (
          <PermissionsPanel
            resourceType="connector"
            resourceId={permissionsTarget.id}
            resourceName={permissionsTarget.name}
            resourceOwnerId={connectors.find(c => c.id === permissionsTarget.id)?.created_by}
            onClose={() => setPermissionsTarget(null)}
          />
        )}
      </div>
      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete connector"
        message={`Delete "${deleteTarget?.name}"? It will be moved to trash and automatically deleted after 7 days.`}
        confirmLabel="Delete"
        destructive
        onConfirm={() => { if (deleteTarget) deleteConnector.mutate(deleteTarget.id); setDeleteTarget(null) }}
        onCancel={() => setDeleteTarget(null)}
      />

      {linkTarget && (
        <Modal
          title="Link connector to warehouse"
          minWidth={460}
          onClose={() => { setLinkTarget(null); setLinkError(null) }}
        >
          <div style={styles.modalBody}>
            <p style={styles.modalText}>
              {tablePermissionsEnabled ? (
                <>
                  Linking <strong>{linkTarget.name}</strong> changes its execution mode: queries run as
                  per-user ClickHouse identities and table grants are enforced by the warehouse.
                  Provisioning begins immediately.
                </>
              ) : (
                <>
                  Linking <strong>{linkTarget.name}</strong> records the warehouse membership.
                  ClickHouse table permissions are currently disabled, so queries keep using the
                  connector's stored credential; per-user identities and table grants begin once an
                  operator enables AETHER_CH_TABLE_PERMISSIONS.
                </>
              )}
            </p>
            <label style={styles.label}>
              Warehouse
              <select
                aria-label="Warehouse"
                style={styles.input}
                value={linkWarehouseId}
                onChange={(e) => setLinkWarehouseId(e.target.value)}
              >
                <option value="">Select warehouse…</option>
                {warehouses.map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.name}
                  </option>
                ))}
              </select>
            </label>
            {warehouses.length === 0 && (
              <p style={styles.modalText}>No warehouses yet. Create one from the Warehouses page.</p>
            )}
            {linkError && <p style={styles.modalError}>{linkError}</p>}
            <div style={styles.modalActions}>
              <button type="button" style={styles.cancelBtn} onClick={() => { setLinkTarget(null); setLinkError(null) }}>
                Cancel
              </button>
              <button
                type="button"
                style={{ ...styles.saveBtn, opacity: !linkWarehouseId ? 0.5 : 1, cursor: !linkWarehouseId ? 'not-allowed' : 'pointer' }}
                disabled={!linkWarehouseId || setWarehouseLink.isPending}
                onClick={() =>
                  setWarehouseLink.mutate({ connectorId: linkTarget.id, warehouseId: linkWarehouseId })
                }
              >
                {setWarehouseLink.isPending ? 'Linking…' : 'Link connector'}
              </button>
            </div>
          </div>
        </Modal>
      )}

      <ConfirmDialog
        open={!!unlinkTarget}
        title="Unlink connector"
        message={tablePermissionsEnabled
          ? `Unlink "${unlinkTarget?.name}" from its warehouse? It returns to shared-credential mode; table grants no longer apply.`
          : `Unlink "${unlinkTarget?.name}" from its warehouse? It returns to shared-credential mode.`}
        confirmLabel="Unlink"
        destructive
        onConfirm={() => {
          if (unlinkTarget) setWarehouseLink.mutate({ connectorId: unlinkTarget.id, warehouseId: null })
        }}
        onCancel={() => setUnlinkTarget(null)}
      />
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  newBtn: { padding: '7px 16px', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  body: { maxWidth: 1100, margin: '0 auto', padding: 'clamp(16px, 4vw, 32px)', width: '100%' },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2 },
  cancelBtn: { padding: '6px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  saveBtn: { padding: '7px 16px', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  managedBadge: {
    display: 'inline-block',
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--accent)',
    background: 'var(--accent-light)',
    border: '1px solid var(--border)',
    borderRadius: 10,
    padding: '2px 8px',
    whiteSpace: 'nowrap' as const,
  },
  sharedBadge: {
    display: 'inline-block',
    fontSize: 11,
    color: 'var(--text-muted)',
    border: '1px solid var(--border)',
    borderRadius: 10,
    padding: '2px 8px',
    whiteSpace: 'nowrap' as const,
  },
  modalBody: {
    padding: '16px 20px',
    display: 'flex',
    flexDirection: 'column' as const,
    gap: 12,
    width: 420,
  },
  modalText: {
    fontSize: 13,
    color: 'var(--text-secondary)',
    lineHeight: 1.5,
    margin: 0,
  },
  modalError: {
    fontSize: 12,
    color: 'var(--error)',
    margin: 0,
  },
  modalActions: {
    display: 'flex',
    justifyContent: 'flex-end',
    gap: 8,
    marginTop: 4,
  },
}
