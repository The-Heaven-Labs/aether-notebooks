import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { EmptyState } from '../components/EmptyState'
import { Server, Loader2, CheckCircle2, XCircle } from 'lucide-react'
import { api } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import type { MCPServerOrg } from '../types/agent'
import { PermissionsPanel } from '../components/PermissionsPanel'

interface MCPForm {
  name: string
  type: 'stdio' | 'http'
  command: string
  args: string
}

const emptyForm = (): MCPForm => ({
  name: '',
  type: 'http',
  command: '',
  args: '',
})

export function MCPPage() {
  useEffect(() => { document.title = "MCP Servers — Heaven's Notebooks" }, [])

  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<MCPForm>(emptyForm())
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [testResults, setTestResults] = useState<Record<string, { success: boolean; message: string }>>({})
  const [testingIds, setTestingIds] = useState<Set<string>>(new Set())
  const [deleteTarget, setDeleteTarget] = useState<MCPServerOrg | null>(null)
  const [permissionsTarget, setPermissionsTarget] = useState<{ id: string; name: string } | null>(null)

  const { data: servers = [], isLoading } = useQuery<MCPServerOrg[]>({
    queryKey: ['mcp-servers'],
    queryFn: () => api.get<MCPServerOrg[]>('/api/v1/mcp-servers'),
  })

  const createMutation = useMutation({
    mutationFn: () => api.post<{ id: string }>('/api/v1/mcp-servers', {
      name: form.name,
      type: form.type,
      command: form.command,
      args: form.args ? form.args.split(' ').filter(Boolean) : [],
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['mcp-servers'] })
      setCreating(false)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const updateMutation = useMutation({
    mutationFn: (id: string) => api.put<{ id: string }>(`/api/v1/mcp-servers/${id}`, {
      name: form.name,
      type: form.type,
      command: form.command,
      args: form.args ? form.args.split(' ').filter(Boolean) : [],
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['mcp-servers'] })
      setEditingId(null)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/mcp-servers/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['mcp-servers'] }),
    onError: (e: unknown) => setDeleteError(String(e)),
  })

  const startEdit = (s: MCPServerOrg) => {
    setEditingId(s.id)
    setForm({
      name: s.name,
      type: s.type as 'stdio' | 'http',
      command: s.command,
      args: s.args?.join(' ') ?? '',
    })
  }

  const testServer = async (id: string) => {
    setTestingIds(prev => new Set(prev).add(id))
    setTestResults(prev => { const next = { ...prev }; delete next[id]; return next })
    try {
      const result = await api.post<{ success: boolean; error?: string; status_code?: number }>(`/api/v1/mcp-servers/${id}/test`, {})
      if (result.success) {
        setTestResults(prev => ({ ...prev, [id]: { success: true, message: `Connected! (status ${result.status_code ?? 'ok'})` } }))
      } else {
        setTestResults(prev => ({ ...prev, [id]: { success: false, message: result.error ?? 'Connection failed' } }))
      }
    } catch (e: unknown) {
      setTestResults(prev => ({ ...prev, [id]: { success: false, message: String(e) } }))
    } finally {
      setTestingIds(prev => { const next = new Set(prev); next.delete(id); return next })
    }
  }

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader
          title="MCP Servers"
          subtitle={servers.length > 0 ? `${servers.length} server${servers.length !== 1 ? 's' : ''}` : ''}
        >
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New MCP Server</button>
        </SectionHeader>

        <div style={styles.info}>
          MCP servers are configured at the organization level and can be shared between multiple agents. Configure MCP servers here, then assign them to agents on the Agents page.
        </div>

        {creating && (
          <FormCard title="New MCP Server">
            <MCPFormFields form={form} setForm={setForm} />
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setCreating(false); setForm(emptyForm()) }}>Cancel</button>
              <button type="button" style={styles.saveBtn} onClick={() => createMutation.mutate()} disabled={!form.name || !form.command || createMutation.isPending}>
                {createMutation.isPending ? 'Creating...' : 'Create'}
              </button>
            </div>
            {formError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</p>}
          </FormCard>
        )}

        {editingId && (
          <FormCard title="Edit MCP Server">
            <MCPFormFields form={form} setForm={setForm} />
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setEditingId(null); setForm(emptyForm()) }}>Cancel</button>
              <button type="button" style={styles.saveBtn} onClick={() => updateMutation.mutate(editingId!)} disabled={!form.name || !form.command || updateMutation.isPending}>
                {updateMutation.isPending ? 'Saving...' : 'Save'}
              </button>
            </div>
            {formError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</p>}
          </FormCard>
        )}

        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}

        {servers.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Server size={28} />}
            title="No MCP servers configured"
            text="MCP servers extend agent capabilities with external tools and data sources."
            action={{ label: '+ New MCP Server', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Type', 'Command', 'Args', '']}>
            {servers.map(s => (
              <tr key={s.id} style={rowStyle}>
                <td style={cellStyle}><strong>{s.name}</strong></td>
                <td style={cellStyle}><code style={styles.badge}>{s.type}</code></td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{s.command}</td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--text-secondary)' }}>{s.args?.join(' ') || '—'}</td>
                <td style={styles.tdActions}>
                  <button type="button" style={styles.permissionsBtn} onClick={() => setPermissionsTarget({ id: s.id, name: s.name })}>Permissions</button>
                  <button type="button" style={styles.testBtn} onClick={() => testServer(s.id)} disabled={testingIds.has(s.id)}>
                    {testingIds.has(s.id) ? <Loader2 size={12} style={{ animation: 'spin 1s linear infinite' }} /> : 'Test'}
                  </button>
                  <button type="button" style={styles.editBtn} onClick={() => startEdit(s)}>Edit</button>
                  <button type="button" style={styles.deleteBtn} onClick={() => setDeleteTarget(s)}>
                    Delete
                  </button>
                  {testResults[s.id] && (
                    <span style={{
                      display: 'inline-flex', alignItems: 'center', gap: 4,
                      fontSize: 11, marginLeft: 6,
                      color: testResults[s.id].success ? 'var(--success, #059669)' : 'var(--error-full)',
                    }}>
                      {testResults[s.id].success ? <CheckCircle2 size={12} /> : <XCircle size={12} />}
                      {testResults[s.id].message}
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </StyledTable>
        )}
      </div>
      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete MCP server"
        message={`Delete "${deleteTarget?.name}"? This cannot be undone.`}
        confirmLabel="Delete"
        destructive
        onConfirm={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget.id); setDeleteTarget(null) }}
        onCancel={() => setDeleteTarget(null)}
      />
      {permissionsTarget && (
        <PermissionsPanel
          resourceType="mcp_server"
          resourceId={permissionsTarget.id}
          resourceName={permissionsTarget.name}
          onClose={() => setPermissionsTarget(null)}
        />
      )}
    </AppShell>
  )
}

function MCPFormFields({ form, setForm }: { form: MCPForm; setForm: React.Dispatch<React.SetStateAction<MCPForm>> }) {
  return (
    <div style={styles.formGrid}>
      <label style={styles.label}>Name
        <input style={styles.input} value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} placeholder="my-mcp-server" />
      </label>
      <label style={styles.label}>Type
        <select style={styles.input} value={form.type} onChange={e => setForm(f => ({ ...f, type: e.target.value as 'stdio' | 'http' }))}>
          <option value="http">HTTP</option>
          <option value="stdio">Stdio</option>
        </select>
      </label>
      <label style={{ ...styles.label, gridColumn: '1 / -1' }}>Command
        <input style={styles.input} value={form.command} onChange={e => setForm(f => ({ ...f, command: e.target.value }))} placeholder="https://example.com/mcp or /usr/local/bin/mcp-server" />
      </label>
      <label style={{ ...styles.label, gridColumn: '1 / -1' }}>Arguments (space-separated)
        <input style={styles.input} value={form.args} onChange={e => setForm(f => ({ ...f, args: e.target.value }))} placeholder="--flag value" />
      </label>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  info: { fontSize: 13, color: 'var(--text-muted)', marginBottom: 16, background: 'var(--bg-secondary)', padding: '10px 14px', borderRadius: 6, border: '1px solid var(--border)' },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--accent-light)', color: 'var(--text-secondary)', padding: '2px 7px', borderRadius: 3 },
  formGrid: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 16 },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2 },
  formActions: { display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 },
  cancelBtn: { padding: '6px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  saveBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  newBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  permissionsBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 },
  testBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6, display: 'inline-flex', alignItems: 'center', gap: 4 },
  editBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--accent)', marginRight: 6 },
  deleteBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--error-full)' },
  tdActions: { padding: '8px 16px', textAlign: 'right' as const },
}

type RowProps = React.CSSProperties
const rowStyle: RowProps = { borderBottom: '1px solid var(--border)' }
const cellStyle: React.CSSProperties = { padding: '10px 16px', verticalAlign: 'top' }

function StyledTable({ headers, children }: { headers: string[]; children: React.ReactNode }) {
  return (
    <table style={{ width: '100%', borderCollapse: 'collapse', border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
      <thead>
        <tr style={{ background: 'var(--bg-secondary)' }}>
          {headers.map(h => <th key={h} style={thStyle}>{h}</th>)}
        </tr>
      </thead>
      <tbody>{children}</tbody>
    </table>
  )
}

const thStyle: React.CSSProperties = { padding: '8px 16px', fontSize: 11, fontWeight: 700, textTransform: 'uppercase' as const, letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }