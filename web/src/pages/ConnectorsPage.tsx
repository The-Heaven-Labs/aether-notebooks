import { useState, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Connector } from '../types'
import { AppShell } from '../components/AppShell'
import { Check, X, Loader2, Star, Database, Pencil, ShieldCheck, Link2, Unlink, Trash2, Zap } from 'lucide-react'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { FormCard } from '../components/FormCard'
import { StatusBadge } from '../components/StatusBadge'
import { SectionHeader } from '../components/SectionHeader'
import { PermissionsPanel } from '../components/PermissionsPanel'
import { EmptyState } from '../components/EmptyState'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { Modal } from '../components/Modal'
import { useAuth } from '../hooks/useAuth'
import { useWarehouseTablePermissions } from '../hooks/useWarehouseTablePermissions'
import { listWarehouses, setConnectorWarehouse } from '../api/warehouses'

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
}

const defaultForm = (): ConnectorForm => ({
  name: '', type: 'postgres', host: 'localhost', port: '5432',
  database: '', user: '', password: '', ssl_mode: 'disable',
  use_tls: false, http_path: '', auth_type: 'pat', token: '',
  client_id: '', client_secret: '', catalog: '', schema: '', stored_auth_type: '',
  is_default: false, timeout_seconds: '0',
  table_allowlist: '', table_denylist: '',
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
  const [testResults, setTestResults] = useState<Record<string, { ok: boolean; error?: string }>>({})
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

  const [autoTested, setAutoTested] = useState(false)

  useEffect(() => {
    if (connectors.length > 0 && !autoTested) {
      setAutoTested(true)
      connectors.forEach(c => testConnector(c.id))
    }
  }, [connectors, autoTested])

  useEffect(() => {
    const editId = searchParams.get('edit')
    if (editId && connectors.length > 0) {
      const c = connectors.find(x => x.id === editId)
      if (c) {
        setEditing(c.id)
        setEditForm({
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
        })
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
      setEditing(null)
      setEditForm(defaultForm())
      setEditError(null)
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
    onSuccess: (connector) => {
      qc.invalidateQueries({ queryKey: ['connectors'] })
      if (formTest) setTestResults((prev) => ({ ...prev, [connector.id]: formTest }))
      setCreating(false)
      setForm(defaultForm())
      setFormTest(null)
      setCreateError(null)
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
    setTestResults((prev) => ({ ...prev, [id]: undefined as unknown as { ok: boolean; error?: string } }))
    setTestingIds((prev) => ({ ...prev, [id]: true }))
    try {
      const result = await api.post<{ ok: boolean; error?: string }>(`/api/v1/connectors/${id}/test`, {})
      setTestResults((prev) => ({ ...prev, [id]: result }))
    } catch (e) {
      setTestResults((prev) => ({ ...prev, [id]: { ok: false, error: String(e) } }))
    } finally {
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

  return (
    <AppShell>
      <div style={styles.body}>
        {!creating && (
          <>
          <SectionHeader title="Connectors" subtitle={connectors.length > 0 ? `${connectors.length} connector${connectors.length !== 1 ? 's' : ''}` : ''}>
            <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Connector</button>
          </SectionHeader>
          <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: -16, marginBottom: 24 }}>
            Connect to your databases (PostgreSQL, ClickHouse, OpenSearch, Databricks) to query data from notebooks.
          </p>
          </>
        )}
        {creating && (
          <FormCard title="New Connector">
            <div style={styles.formGrid}>
              <label style={styles.label}>Name
                <input style={styles.input} value={form.name} onChange={setField('name')} placeholder="My Postgres" />
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
              <label style={styles.label}>Query Timeout (s)
                <input style={styles.input} type="text" value={form.timeout_seconds}
                  onChange={(e) => setForm(f => ({ ...f, timeout_seconds: e.target.value }))} placeholder="0 = unlimited" />
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)', gridColumn: '1 / -1' }}>
                <input type="checkbox" checked={form.is_default ?? false}
                  onChange={e => setForm(f => ({ ...f, is_default: e.target.checked }))} />
                Set as default connector for new notebooks
              </label>
            </div>
            <div style={styles.formActions}>
              <button
                type="button"
                style={styles.testBtn}
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
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setCreating(false); setForm(defaultForm()); setFormTest(null) }}>Cancel</button>
              <button
                type="button"
                style={styles.saveBtn}
                onClick={() => createConnector.mutate()}
                disabled={!canSubmitConnector(form, false) || createConnector.isPending}
                title={!form.name ? 'Name is required' : connectorFormMissingField(form, false)}
              >
                {createConnector.isPending ? 'Creating…' : 'Create'}
              </button>
            </div>
            {createError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{createError}</p>}
          </FormCard>
        )}

        {editing && (
          <FormCard title="Edit Connector">
            <div style={styles.formGrid}>
              <label style={styles.label}>Name
                <input style={styles.input} value={editForm.name} onChange={(e) => setEditForm(f => ({ ...f, name: e.target.value }))} />
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
            </div>
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setEditing(null); setEditForm(defaultForm()); setEditError(null) }}>Cancel</button>
              <button
                type="button"
                style={styles.saveBtn}
                onClick={() => updateConnector.mutate(editing!)}
                disabled={!canSubmitConnector(editForm, true) || updateConnector.isPending}
                title={!editForm.name ? 'Name is required' : connectorFormMissingField(editForm, true)}
              >
                {updateConnector.isPending ? 'Saving…' : 'Save'}
              </button>
            </div>
            {editError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{editError}</p>}
          </FormCard>
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
            headers={['Name', 'Type', 'Host', 'Database', 'Access', 'Status', <span className="sr-only">Actions</span>]}
            headerClassNames={[undefined, undefined, undefined, undefined, undefined, undefined, 'connector-actions-header']}
          >
            {connectors.map((c) => {
              const test = testResults[c.id]
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
                  <td style={cellStyle}><code style={styles.badge}>{c.type}</code></td>
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
                    ) : test ? (
                      <StatusBadge
                        status={test.ok ? 'success' : 'error'}
                        label={test.ok ? 'Connected' : (test.error ?? 'Failed')}
                        icon={test.ok ? <Check size={12} /> : <X size={12} />}
                      />
                    ) : (
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>
                        Unknown — click Test
                      </span>
                    )}
                  </td>
                  <td className="connector-actions-cell">
                    <div className="connector-actions-grid">
                      <button type="button" className="connector-action" title="Test connection" aria-label="Test connection" onClick={() => testConnector(c.id)} disabled={testingIds[c.id]}>
                        {testingIds[c.id] ? <Loader2 size={13} className="connector-action-spin" /> : <Zap size={13} />}
                      </button>
                      <button type="button" className="connector-action connector-action--accent" title="Edit connector" aria-label="Edit connector" onClick={() => {
                        setEditing(c.id)
                        setEditForm({
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
                        })
                      }}>
                        <Pencil size={13} />
                      </button>
                      <span className="connector-actions-break" aria-hidden="true" />
                      <button type="button" className="connector-action" title="Permissions" aria-label="Permissions" onClick={() => setPermissionsTarget({ type: 'connector', id: c.id, name: c.name })}>
                        <ShieldCheck size={13} />
                      </button>
                      {isAdmin && c.type === 'clickhouse' && (
                        c.warehouse_id ? (
                          <button type="button" className="connector-action" title="Unlink from warehouse" aria-label="Unlink from warehouse" onClick={() => setUnlinkTarget(c)}>
                            <Unlink size={13} />
                          </button>
                        ) : (
                          <button type="button" className="connector-action" title="Link to warehouse" aria-label="Link to warehouse" onClick={() => { setLinkTarget(c); setLinkWarehouseId(''); setLinkError(null) }}>
                            <Link2 size={13} />
                          </button>
                        )
                      )}
                      <span className="connector-actions-break" aria-hidden="true" />
                      {!c.is_default && (
                        <button type="button" className="connector-action" title="Set as default connector for new notebooks" aria-label="Set as default connector" onClick={() => setDefault.mutate(c.id)}>
                          <Star size={13} />
                        </button>
                      )}
                      <button type="button" className="connector-action connector-action--danger" title="Delete connector" aria-label="Delete connector" onClick={() => setDeleteTarget(c)}>
                        <Trash2 size={13} />
                      </button>
                    </div>
                  </td>
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
  newBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  body: { maxWidth: 1100, margin: '0 auto', padding: 'clamp(16px, 4vw, 32px)', width: '100%' },
  formGrid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 12, marginBottom: 16 },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2 },
  formActions: { display: 'flex', gap: 8, justifyContent: 'flex-end' },
  testBtn: { padding: '6px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)', fontWeight: 600 },
  cancelBtn: { padding: '6px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  saveBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--accent-light)', color: 'var(--text-secondary)', padding: '2px 7px', borderRadius: 3 },
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
