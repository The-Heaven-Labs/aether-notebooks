import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { EmptyState } from '../components/EmptyState'
import { Wrench } from 'lucide-react'
import { api } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import type { Tool, ToolType } from '../types/agent'
import { PermissionsPanel } from '../components/PermissionsPanel'

interface ParamDef {
  name: string
  type: string
  description: string
  required: boolean
}

interface ToolForm {
  name: string
  description: string
  type: ToolType
  url: string
  method: string
  headers: { key: string; value: string }[]
  connector_id: string
  sql: string
  parameters: ParamDef[]
}

interface Connector {
  id: string
  name: string
  type: string
}

const emptyForm = (): ToolForm => ({
  name: '',
  description: '',
  type: 'webhook',
  url: '',
  method: 'POST',
  headers: [{ key: '', value: '' }],
  connector_id: '',
  sql: '',
  parameters: [],
})

const PARAM_TYPES = ['string', 'number', 'boolean', 'integer']

const METHOD_OPTIONS = ['GET', 'POST', 'PUT']
const TYPE_OPTIONS: { value: ToolType; label: string }[] = [
  { value: 'webhook', label: 'Webhook' },
  { value: 'sql_query', label: 'SQL Query' },
  { value: 'builtin', label: 'Built-in' },
]

const TYPE_COLORS: Record<ToolType, string> = {
  webhook: '#3b82f6',
  sql_query: '#22c55e',
  builtin: '#a855f7',
}

export function ToolsPage() {
  useEffect(() => { document.title = "Tools — Heaven's Notebooks" }, [])

  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<ToolForm>(emptyForm())
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; name: string } | null>(null)
  const [permissionsTarget, setPermissionsTarget] = useState<{ id: string; name: string } | null>(null)
  const [testResult, setTestResult] = useState<{ id: string; ok: boolean; message: string } | null>(null)

  const { data: tools = [], isLoading } = useQuery<Tool[]>({
    queryKey: ['tools'],
    queryFn: () => api.get<Tool[]>('/api/v1/tools'),
  })

  const { data: connectors = [] } = useQuery<Connector[]>({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })

  const buildPayload = () => {
    const payload: Record<string, any> = {
      name: form.name,
      description: form.description,
      type: form.type,
    }
    if (form.parameters.length > 0) {
      const props: Record<string, any> = {}
      const required: string[] = []
      for (const p of form.parameters) {
        props[p.name] = { type: p.type, description: p.description }
        if (p.required) required.push(p.name)
      }
      payload.schema = { type: 'object', properties: props }
      if (required.length > 0) payload.schema.required = required
    } else {
      payload.schema = {}
    }
    if (form.type === 'webhook') {
      payload.config = {
        url: form.url,
        method: form.method,
        headers: form.headers.filter(h => h.key && h.value).reduce((acc, h) => ({ ...acc, [h.key]: h.value }), {}),
      }
    } else if (form.type === 'sql_query') {
      payload.config = {
        connector_id: form.connector_id,
        query: form.sql,
      }
    }
    return payload
  }

  const createMutation = useMutation({
    mutationFn: () => api.post<{ id: string }>('/api/v1/tools', buildPayload()),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['tools'] })
      setCreating(false)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const updateMutation = useMutation({
    mutationFn: (id: string) => api.put<{ id: string }>(`/api/v1/tools/${id}`, buildPayload()),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['tools'] })
      setEditingId(null)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/tools/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tools'] }),
    onError: (e: unknown) => setDeleteError(String(e)),
  })

  const testMutation = useMutation({
    mutationFn: async (id: string) => {
      const res = await api.post<{ status: number; body?: string; result?: any }>(`/api/v1/tools/${id}/test`, {})
      return res
    },
    onSuccess: (data, id) => {
      setTestResult({ id, ok: true, message: `Status ${data.status}${data.body ? ` — ${data.body}` : ''}` })
    },
    onError: (e: unknown, id) => {
      setTestResult({ id, ok: false, message: String(e) })
    },
  })

  const startEdit = (tool: Tool) => {
    setEditingId(tool.id)
    const config = tool.config || {}
    const schema = tool.schema || {}
    const schemaProps = schema.properties as Record<string, { type: string; description?: string }> | undefined
    const required = (schema.required as string[]) || []
    const parameters: ParamDef[] = schemaProps
      ? Object.entries(schemaProps).map(([name, def]) => ({
          name,
          type: def.type || 'string',
          description: def.description || '',
          required: required.includes(name),
        }))
      : []
    setForm({
      name: tool.name,
      description: tool.description ?? '',
      type: tool.type,
      url: config.url ?? '',
      method: config.method ?? 'POST',
      headers: typeof config.headers === 'object' && config.headers
        ? Object.entries(config.headers).map(([key, value]) => ({ key, value: String(value) }))
        : [{ key: '', value: '' }],
      connector_id: config.connector_id ?? '',
      sql: config.query ?? config.sql ?? '',
      parameters,
    })
  }

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader title="Tools" subtitle={tools.length > 0 ? `${tools.length} tool${tools.length !== 1 ? 's' : ''}` : ''}>
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Tool</button>
        </SectionHeader>

        {creating && (
          <FormCard title="New Tool">
            <ToolFormFields form={form} setForm={setForm} connectors={connectors} editing={false} />
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setCreating(false); setForm(emptyForm()) }}>Cancel</button>
              <button type="button" style={styles.saveBtn} onClick={() => createMutation.mutate()} disabled={!form.name || createMutation.isPending}>
                {createMutation.isPending ? 'Creating…' : 'Create'}
              </button>
            </div>
            {formError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</p>}
          </FormCard>
        )}

        {editingId && (
          <FormCard title="Edit Tool">
            <ToolFormFields form={form} setForm={setForm} connectors={connectors} editing />
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setEditingId(null); setForm(emptyForm()) }}>Cancel</button>
              <button type="button" style={styles.saveBtn} onClick={() => updateMutation.mutate(editingId!)} disabled={!form.name || updateMutation.isPending}>
                {updateMutation.isPending ? 'Saving…' : 'Save'}
              </button>
            </div>
            {formError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</p>}
          </FormCard>
        )}

        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}

        {tools.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Wrench size={28} />}
            title="No tools yet"
            text="Tools are reusable capabilities (webhooks, SQL queries, or built-in functions) that agents can use."
            action={{ label: '+ New Tool', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Type', 'Description', '']}>
            {tools.map((t) => (
              <tr key={t.id} style={rowStyle}>
                <td style={cellStyle}><strong>{t.name}</strong></td>
                <td style={cellStyle}>
                  <span style={{ ...styles.typeBadge, background: TYPE_COLORS[t.type] + '20', color: TYPE_COLORS[t.type], borderColor: TYPE_COLORS[t.type] + '40' }}>
                    {t.type.replace('_', ' ')}
                  </span>
                </td>
                <td style={cellStyle}><span style={{ color: 'var(--text-secondary)', fontSize: 13 }}>{t.description || '—'}</span></td>
                <td style={styles.tdActions}>
                  <button type="button" style={styles.permissionsBtn} onClick={() => setPermissionsTarget({ id: t.id, name: t.name })}>Permissions</button>
                  {t.type !== 'builtin' && (
                    <>
                      <button
                        type="button"
                        style={testMutation.isPending && testResult?.id === t.id ? { ...styles.testBtn, opacity: 0.6 } : styles.testBtn}
                        onClick={() => { setTestResult(null); testMutation.mutate(t.id) }}
                        disabled={testMutation.isPending}
                      >
                        {testMutation.isPending && testResult?.id === t.id ? 'Testing…' : 'Test'}
                      </button>
                      <button type="button" style={styles.editBtn} onClick={() => startEdit(t)}>Edit</button>
                    </>
                  )}
                  {t.type !== 'builtin' && (
                    <button type="button" style={styles.deleteBtn} onClick={() => setDeleteTarget({ id: t.id, name: t.name })}>
                      Delete
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </StyledTable>
        )}

        {testResult && (
          <div style={{
            marginTop: 12,
            padding: '10px 14px',
            borderRadius: 6,
            background: testResult.ok ? 'rgba(34,197,94,0.1)' : 'rgba(239,68,68,0.1)',
            border: `1px solid ${testResult.ok ? 'rgba(34,197,94,0.3)' : 'rgba(239,68,68,0.3)'}`,
            color: testResult.ok ? '#22c55e' : 'var(--error-full)',
            fontSize: 13,
          }}>
            {testResult.ok ? '✅ OK: ' : '❌ Failed: '}{testResult.message}
          </div>
        )}
      </div>
      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete tool"
        message={`Delete "${deleteTarget?.name}"? This cannot be undone.`}
        confirmLabel="Delete"
        destructive
        onConfirm={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget.id); setDeleteTarget(null) }}
        onCancel={() => setDeleteTarget(null)}
      />
      {permissionsTarget && (
        <PermissionsPanel
          resourceType="tool"
          resourceId={permissionsTarget.id}
          resourceName={permissionsTarget.name}
          onClose={() => setPermissionsTarget(null)}
        />
      )}
    </AppShell>
  )
}

function ToolFormFields({ form, setForm, connectors, editing }: {
  form: ToolForm
  setForm: React.Dispatch<React.SetStateAction<ToolForm>>
  connectors: Connector[]
  editing: boolean
}) {
  const setField = (field: keyof ToolForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) =>
    setForm(f => ({ ...f, [field]: e.target.value }))

  const setParam = (i: number, field: keyof ParamDef, val: string | boolean) => setForm(f => {
    const p = [...f.parameters]
    p[i] = { ...p[i], [field]: val }
    return { ...f, parameters: p }
  })
  const addParam = () => setForm(f => ({ ...f, parameters: [...f.parameters, { name: '', type: 'string', description: '', required: false }] }))
  const removeParam = (i: number) => setForm(f => ({ ...f, parameters: f.parameters.filter((_, idx) => idx !== i) }))

  const addHeader = () => setForm(f => ({ ...f, headers: [...f.headers, { key: '', value: '' }] }))
  const removeHeader = (i: number) => setForm(f => ({ ...f, headers: f.headers.filter((_, idx) => idx !== i) }))
  const updateHeader = (i: number, field: 'key' | 'value', val: string) => setForm(f => {
    const h = [...f.headers]
    h[i] = { ...h[i], [field]: val }
    return { ...f, headers: h }
  })

  return (
    <div style={styles.formGrid}>
      <label style={styles.label}>Name
        <input style={styles.input} value={form.name} onChange={setField('name')} placeholder="My Webhook" />
      </label>
      <label style={styles.label}>Description
        <input style={styles.input} value={form.description} onChange={setField('description')} placeholder="Sends data to external API" />
      </label>
      <label style={styles.label}>Type
        <select style={styles.input} value={form.type} onChange={e => setForm(f => ({ ...f, type: e.target.value as ToolType }))} disabled={editing}>
          {TYPE_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
      </label>

      {form.type === 'builtin' && (
        <div style={{ gridColumn: '1 / -1', padding: '12px 14px', background: 'var(--bg-secondary)', borderRadius: 6, color: 'var(--text-secondary)', fontSize: 13, lineHeight: 1.6 }}>
          Built-in tools are predefined system tools. You can view and test them, but cannot create or edit them.
        </div>
      )}

      {form.type === 'webhook' && (
        <>
          <label style={styles.label}>URL
            <input style={styles.input} value={form.url} onChange={setField('url')} placeholder="https://example.com/api/hook" />
          </label>
          <label style={styles.label}>Method
            <select style={styles.input} value={form.method} onChange={setField('method')}>
              {METHOD_OPTIONS.map(m => <option key={m} value={m}>{m}</option>)}
            </select>
          </label>
          <label style={{ ...styles.label, gridColumn: '1 / -1' }}>
            Headers
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 4 }}>
              {form.headers.map((h, i) => (
                <div key={i} style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                  <input style={{ ...styles.input, flex: 1 }} placeholder="Key" value={h.key} onChange={e => updateHeader(i, 'key', e.target.value)} />
                  <input style={{ ...styles.input, flex: 1 }} placeholder="Value" value={h.value} onChange={e => updateHeader(i, 'value', e.target.value)} />
                  <button type="button" style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--error-full)', fontSize: 14, padding: '4px 6px' }} onClick={() => removeHeader(i)}>×</button>
                </div>
              ))}
              <button type="button" style={{ ...styles.cancelBtn, alignSelf: 'flex-start' }} onClick={addHeader}>+ Add Header</button>
            </div>
          </label>
        </>
      )}

      {form.type === 'sql_query' && (
        <>
          <label style={styles.label}>Connector
            <select style={styles.input} value={form.connector_id} onChange={setField('connector_id')}>
              <option value="">Select a connector…</option>
              {connectors.map(c => <option key={c.id} value={c.id}>{c.name} ({c.type})</option>)}
            </select>
          </label>
          <label style={{ ...styles.label, gridColumn: '1 / -1' }}>SQL Query
            <textarea style={{ ...styles.input, minHeight: 120, resize: 'vertical', fontFamily: 'var(--font-mono)' }} value={form.sql} onChange={setField('sql')} placeholder="SELECT * FROM table LIMIT 10" />
          </label>
        </>
      )}

      {form.type !== 'builtin' && (
        <label style={{ ...styles.label, gridColumn: '1 / -1', marginTop: 4 }}>
          Parameters
          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>
            Define the arguments the LLM can pass to this tool. Use <code>{'{{name}}'}</code> in SQL queries to reference parameters.
          </div>
          {form.parameters.length === 0 ? (
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 6 }}>No parameters defined — tool will be called without arguments.</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 6 }}>
              {form.parameters.map((p, i) => (
                <div key={i} style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
                  <input style={{ ...styles.input, width: 120 }} placeholder="name" value={p.name} onChange={e => setParam(i, 'name', e.target.value)} />
                  <select style={{ ...styles.input, width: 100 }} value={p.type} onChange={e => setParam(i, 'type', e.target.value)}>
                    {PARAM_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
                  </select>
                  <input style={{ ...styles.input, flex: 1, minWidth: 150 }} placeholder="description" value={p.description} onChange={e => setParam(i, 'description', e.target.value)} />
                  <label style={{ fontSize: 12, display: 'flex', alignItems: 'center', gap: 4, color: 'var(--text-secondary)' }}>
                    <input type="checkbox" checked={p.required} onChange={e => setParam(i, 'required', e.target.checked)} />
                    required
                  </label>
                  <button type="button" style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--error-full)', fontSize: 14, padding: '4px 6px' }} onClick={() => removeParam(i)}>×</button>
                </div>
              ))}
            </div>
          )}
          <button type="button" style={{ ...styles.cancelBtn, alignSelf: 'flex-start' }} onClick={addParam}>+ Add Parameter</button>
        </label>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  formGrid: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 16 },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2 },
  formActions: { display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 },
  cancelBtn: { padding: '6px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  saveBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  newBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  permissionsBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 },
  editBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--accent)', marginRight: 6 },
  deleteBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--error-full)' },
  testBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 },
  tdActions: { padding: '8px 16px', textAlign: 'right' as const },
  typeBadge: { display: 'inline-block', fontSize: 11, fontWeight: 600, padding: '2px 10px', borderRadius: 4, border: '1px solid', textTransform: 'capitalize' as const },
}

type RowProps = React.CSSProperties
const rowStyle: RowProps = { borderBottom: '1px solid var(--border)' }
const cellStyle: React.CSSProperties = { padding: '10px 16px', verticalAlign: 'top' }

function StyledTable({ headers, children }: { headers: string[]; children: React.ReactNode }) {
  return (
    <table style={{ width: '100%', borderCollapse: 'collapse', border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
      <thead>
        <tr style={{ background: 'var(--bg-secondary)' }}>
          {headers.map(h => <th key={h} style={{ ...thStyle, textAlign: 'left' }}>{h}</th>)}
        </tr>
      </thead>
      <tbody>{children}</tbody>
    </table>
  )
}

const thStyle: React.CSSProperties = { padding: '8px 16px', fontSize: 11, fontWeight: 700, textTransform: 'uppercase' as const, letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }
