import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { EmptyState } from '../components/EmptyState'
import { StatusBadge } from '../components/StatusBadge'
import { Server, Check, Pencil, ShieldCheck, Trash2, X, Zap } from 'lucide-react'
import { api } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { FormModal } from '../components/FormModal'
import { RowAction, RowActionsBreak, RowActionsCell, RowActionsHeader } from '../components/RowActions'
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
  useEffect(() => { document.title = "MCP Servers — Aether Notebooks" }, [])

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
  const nameRef = useRef<HTMLInputElement>(null)

  const { data: servers = [], isLoading } = useQuery<MCPServerOrg[]>({
    queryKey: ['mcp-servers'],
    queryFn: () => api.get<MCPServerOrg[]>('/api/v1/mcp-servers'),
  })

  const closeCreate = () => {
    setCreating(false)
    setForm(emptyForm())
    setFormError(null)
  }

  const closeEdit = () => {
    setEditingId(null)
    setForm(emptyForm())
    setFormError(null)
  }

  const createMutation = useMutation({
    mutationFn: () => api.post<{ id: string }>('/api/v1/mcp-servers', {
      name: form.name,
      type: form.type,
      command: form.command,
      args: form.args ? form.args.split(' ').filter(Boolean) : [],
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['mcp-servers'] })
      closeCreate()
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
      closeEdit()
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
    setFormError(null)
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

  const editingServer = servers.find((s) => s.id === editingId)

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
          <FormModal
            title="New MCP Server"
            onClose={closeCreate}
            initialFocusRef={nameRef}
            width="min(560px, 92vw)"
            error={formError}
            submitLabel="Create"
            pendingLabel="Creating…"
            pending={createMutation.isPending}
            submitDisabled={!form.name || !form.command}
            submitTitle={!form.name ? 'Name is required' : !form.command ? 'Command is required' : undefined}
            onSubmit={() => createMutation.mutate()}
          >
            <MCPFormFields form={form} setForm={setForm} nameRef={nameRef} />
          </FormModal>
        )}

        {editingId && (
          <FormModal
            title={`Edit "${editingServer?.name ?? 'MCP server'}"`}
            onClose={closeEdit}
            initialFocusRef={nameRef}
            width="min(560px, 92vw)"
            error={formError}
            submitLabel="Save"
            pendingLabel="Saving…"
            pending={updateMutation.isPending}
            submitDisabled={!form.name || !form.command}
            submitTitle={!form.name ? 'Name is required' : !form.command ? 'Command is required' : undefined}
            onSubmit={() => updateMutation.mutate(editingId!)}
          >
            <MCPFormFields form={form} setForm={setForm} nameRef={nameRef} />
          </FormModal>
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
          <StyledTable
            headers={['Name', 'Type', 'Command', 'Args', 'Status', <RowActionsHeader />]}
            headerClassNames={[undefined, undefined, undefined, undefined, undefined, 'row-actions-header']}
          >
            {servers.map(s => {
              const testing = testingIds.has(s.id)
              const test = testResults[s.id]
              return (
                <tr key={s.id} style={rowStyle}>
                  <td style={cellStyle}><strong>{s.name}</strong></td>
                  <td style={cellStyle}><code style={styles.badge}>{s.type}</code></td>
                  <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{s.command}</td>
                  <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--text-secondary)' }}>{s.args?.join(' ') || '—'}</td>
                  <td style={cellStyle}>
                    {testing ? (
                      <StatusBadge status="neutral" label="Testing…" />
                    ) : test ? (
                      <StatusBadge
                        status={test.success ? 'success' : 'error'}
                        label={test.success ? 'Connected' : 'Failed'}
                        icon={test.success ? <Check size={12} /> : <X size={12} />}
                        title={test.message}
                      />
                    ) : (
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>
                        Unknown — click Test
                      </span>
                    )}
                  </td>
                  <RowActionsCell>
                    <RowAction label="Test MCP server" icon={<Zap size={13} />} spinning={testing} disabled={testing} onClick={() => testServer(s.id)} />
                    <RowAction label="Edit MCP server" icon={<Pencil size={13} />} accent onClick={() => startEdit(s)} />
                    <RowActionsBreak />
                    <RowAction label="Permissions" icon={<ShieldCheck size={13} />} onClick={() => setPermissionsTarget({ id: s.id, name: s.name })} />
                    <RowAction label="Delete MCP server" icon={<Trash2 size={13} />} danger onClick={() => setDeleteTarget(s)} />
                  </RowActionsCell>
                </tr>
              )
            })}
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

function MCPFormFields({ form, setForm, nameRef }: {
  form: MCPForm
  setForm: React.Dispatch<React.SetStateAction<MCPForm>>
  nameRef: React.RefObject<HTMLInputElement | null>
}) {
  return (
    <>
      <label style={styles.label}>Name
        <input ref={nameRef} style={styles.input} value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} placeholder="my-mcp-server" />
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
    </>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  info: { fontSize: 13, color: 'var(--text-muted)', marginBottom: 16, background: 'var(--bg-secondary)', padding: '10px 14px', borderRadius: 6, border: '1px solid var(--border)' },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--accent-light)', color: 'var(--text-secondary)', padding: '2px 7px', borderRadius: 3 },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2 },
  newBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
}
