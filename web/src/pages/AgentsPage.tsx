import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { api } from '../api/client'
import type { Agent, ModelConfig, Skill, MCPServerOrg } from '../types/agent'

interface AgentForm {
  name: string
  description: string
  system_prompt: string
  model_config_id: string
  subagent_model_config_id: string
  skill_ids: string[]
  mcp_server_ids: string[]
}

const emptyForm = (): AgentForm => ({
  name: '',
  description: '',
  system_prompt: '',
  model_config_id: '',
  subagent_model_config_id: '',
  skill_ids: [],
  mcp_server_ids: [],
})

export function AgentsPage() {
  useEffect(() => { document.title = "Agents — Heaven's Notebooks" }, [])

  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<AgentForm>(emptyForm())
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const { data: agents = [], isLoading } = useQuery<Agent[]>({
    queryKey: ['agents'],
    queryFn: () => api.get<Agent[]>('/api/v1/agents'),
  })

  const { data: modelConfigs = [] } = useQuery<ModelConfig[]>({
    queryKey: ['model-configs'],
    queryFn: () => api.get<ModelConfig[]>('/api/v1/model-configs'),
  })

  const { data: skills = [] } = useQuery<Skill[]>({
    queryKey: ['skills'],
    queryFn: () => api.get<Skill[]>('/api/v1/skills'),
  })

  const { data: mcpServers = [] } = useQuery<MCPServerOrg[]>({
    queryKey: ['mcp-servers'],
    queryFn: () => api.get<MCPServerOrg[]>('/api/v1/mcp-servers'),
  })

  const createMutation = useMutation({
    mutationFn: () => api.post<{ id: string }>('/api/v1/agents', {
      name: form.name,
      description: form.description || undefined,
      system_prompt: form.system_prompt || undefined,
      model_config_id: form.model_config_id || undefined,
      subagent_model_config_id: form.subagent_model_config_id || undefined,
      skill_ids: form.skill_ids,
      mcp_server_ids: form.mcp_server_ids,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['agents'] })
      setCreating(false)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const updateMutation = useMutation({
    mutationFn: (id: string) => api.put<{ id: string }>(`/api/v1/agents/${id}`, {
      name: form.name,
      description: form.description || undefined,
      system_prompt: form.system_prompt || undefined,
      model_config_id: form.model_config_id || undefined,
      subagent_model_config_id: form.subagent_model_config_id || undefined,
      skill_ids: form.skill_ids,
      mcp_server_ids: form.mcp_server_ids,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['agents'] })
      setEditingId(null)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/agents/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['agents'] }),
    onError: (e: unknown) => setDeleteError(String(e)),
  })

  const startEdit = (agent: Agent) => {
    setEditingId(agent.id)
    setForm({
      name: agent.name,
      description: agent.description ?? '',
      system_prompt: agent.system_prompt ?? '',
      model_config_id: agent.model_config_id ?? '',
      subagent_model_config_id: agent.subagent_model_config_id ?? '',
      skill_ids: agent.skill_ids ?? [],
      mcp_server_ids: agent.mcp_server_ids ?? [],
    })
  }

  const toggleSkill = (id: string) => setForm(f => ({
    ...f,
    skill_ids: f.skill_ids.includes(id) ? f.skill_ids.filter(s => s !== id) : [...f.skill_ids, id],
  }))

  const toggleMCPServer = (id: string) => setForm(f => ({
    ...f,
    mcp_server_ids: f.mcp_server_ids.includes(id) ? f.mcp_server_ids.filter(s => s !== id) : [...f.mcp_server_ids, id],
  }))

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader
          title="Agents"
          subtitle={agents.length > 0 ? `${agents.length} agent${agents.length !== 1 ? 's' : ''}` : ''}
        >
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Agent</button>
        </SectionHeader>

        {creating && (
          <FormCard title="New Agent">
            <AgentFormFields
              form={form} setForm={setForm}
              modelConfigs={modelConfigs} skills={skills} mcpServers={mcpServers}
              toggleSkill={toggleSkill} toggleMCPServer={toggleMCPServer}
            />
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setCreating(false); setForm(emptyForm()) }}>Cancel</button>
              <button type="button" style={styles.saveBtn} onClick={() => createMutation.mutate()} disabled={!form.name || createMutation.isPending}>
                {createMutation.isPending ? 'Creating...' : 'Create'}
              </button>
            </div>
            {formError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</p>}
          </FormCard>
        )}

        {editingId && (
          <FormCard title="Edit Agent">
            <AgentFormFields
              form={form} setForm={setForm}
              modelConfigs={modelConfigs} skills={skills} mcpServers={mcpServers}
              toggleSkill={toggleSkill} toggleMCPServer={toggleMCPServer}
            />
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setEditingId(null); setForm(emptyForm()) }}>Cancel</button>
              <button type="button" style={styles.saveBtn} onClick={() => updateMutation.mutate(editingId!)} disabled={!form.name || updateMutation.isPending}>
                {updateMutation.isPending ? 'Saving...' : 'Save'}
              </button>
            </div>
            {formError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</p>}
          </FormCard>
        )}

        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}

        <StyledTable headers={['Name', 'Model Config', 'Skills', 'MCP Servers', '']}>
          {agents.map((a) => {
            const mc = modelConfigs.find(m => m.id === a.model_config_id)
            return (
              <tr key={a.id} style={rowStyle}>
                <td style={cellStyle}>
                  <strong>{a.name}</strong>
                  {a.description && <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginTop: 2 }}>{a.description}</div>}
                </td>
                <td style={cellStyle}>
                  {mc ? <span style={styles.badge}>{mc.name}</span> : <span style={{ color: 'var(--text-muted)' }}>default</span>}
                </td>
                <td style={cellStyle}>
                  {a.skill_ids?.length
                    ? a.skill_ids.map(id => skills.find(s => s.id === id)?.name ?? id).filter(Boolean).join(', ') || '—'
                    : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                </td>
                <td style={cellStyle}>
                  {a.mcp_servers?.length
                    ? a.mcp_servers.map(m => m.name).join(', ')
                    : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                </td>
                <td style={styles.tdActions}>
                  <button type="button" style={styles.editBtn} onClick={() => startEdit(a)}>Edit</button>
                  <button type="button" style={styles.deleteBtn} onClick={() => { if (confirm(`Delete "${a.name}"?`)) deleteMutation.mutate(a.id) }}>
                    Delete
                  </button>
                </td>
              </tr>
            )
          })}
          {agents.length === 0 && !isLoading && (
            <tr>
              <td colSpan={5} style={{ ...cellStyle, textAlign: 'center', color: 'var(--text-muted)', padding: 40 }}>
                No agents yet. Create one to get started with AI-powered notebooks.
              </td>
            </tr>
          )}
        </StyledTable>
      </div>
    </AppShell>
  )
}

function AgentFormFields({ form, setForm, modelConfigs, skills, mcpServers, toggleSkill, toggleMCPServer }: {
  form: AgentForm
  setForm: React.Dispatch<React.SetStateAction<AgentForm>>
  modelConfigs: ModelConfig[]
  skills: Skill[]
  mcpServers: MCPServerOrg[]
  toggleSkill: (id: string) => void
  toggleMCPServer: (id: string) => void
}) {
  return (
    <div style={styles.formGrid}>
      <label style={styles.label}>Name
        <input style={styles.input} value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} placeholder="Data Analyst" />
      </label>
      <label style={styles.label}>Description
        <input style={styles.input} value={form.description} onChange={e => setForm(f => ({ ...f, description: e.target.value }))} placeholder="Answers questions about your data" />
      </label>
      <label style={{ ...styles.label, gridColumn: '1 / -1' }}>System Prompt
        <textarea
          style={{ ...styles.input, minHeight: 80, resize: 'vertical' }}
          value={form.system_prompt}
          onChange={e => setForm(f => ({ ...f, system_prompt: e.target.value }))}
          placeholder="You are a helpful data analyst..."
        />
      </label>
      <label style={styles.label}>Model
        <select style={styles.input} value={form.model_config_id} onChange={e => setForm(f => ({ ...f, model_config_id: e.target.value }))}>
          <option value="">— Default —</option>
          {modelConfigs.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}
        </select>
      </label>
      <label style={styles.label}>Subagent Model
        <select style={styles.input} value={form.subagent_model_config_id} onChange={e => setForm(f => ({ ...f, subagent_model_config_id: e.target.value }))}>
          <option value="">— Default —</option>
          {modelConfigs.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}
        </select>
      </label>
      <label style={{ ...styles.label, gridColumn: '1 / -1' }}>
        Skills
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 6 }}>
          {skills.length === 0 && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No skills yet</span>}
          {skills.map(s => (
            <button
              key={s.id}
              type="button"
              style={{
                padding: '3px 10px', fontSize: 12, borderRadius: 12, cursor: 'pointer',
                background: form.skill_ids.includes(s.id) ? 'var(--accent)' : 'var(--bg-input)',
                color: form.skill_ids.includes(s.id) ? '#fff' : 'var(--text-secondary)',
                border: `1px solid ${form.skill_ids.includes(s.id) ? 'var(--accent)' : 'var(--border)'}`,
                fontWeight: 500,
              }}
              onClick={() => toggleSkill(s.id)}
            >
              {s.name}
            </button>
          ))}
        </div>
      </label>
      <label style={{ ...styles.label, gridColumn: '1 / -1' }}>
        MCP Servers
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 6 }}>
          {mcpServers.length === 0 && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No MCP servers configured — <a href="/mcps" style={{ color: 'var(--accent)' }}>create one</a></span>}
          {mcpServers.map(m => (
            <button
              key={m.id}
              type="button"
              style={{
                padding: '3px 10px', fontSize: 12, borderRadius: 12, cursor: 'pointer',
                background: form.mcp_server_ids.includes(m.id) ? 'var(--accent)' : 'var(--bg-input)',
                color: form.mcp_server_ids.includes(m.id) ? '#fff' : 'var(--text-secondary)',
                border: `1px solid ${form.mcp_server_ids.includes(m.id) ? 'var(--accent)' : 'var(--border)'}`,
                fontWeight: 500,
              }}
              onClick={() => toggleMCPServer(m.id)}
            >
              {m.name} <span style={{ opacity: 0.7, fontSize: 10 }}>{m.type}</span>
            </button>
          ))}
        </div>
      </label>
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
  editBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--accent)', marginRight: 6 },
  deleteBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--error-full)' },
  tdActions: { padding: '8px 16px', textAlign: 'right' as const },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--accent-light)', color: 'var(--text-secondary)', padding: '2px 7px', borderRadius: 3 },
}

const rowStyle: React.CSSProperties = { borderBottom: '1px solid var(--border)' }
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

const thStyle: React.CSSProperties = {
  padding: '8px 16px',
  fontSize: 11,
  fontWeight: 700,
  textTransform: 'uppercase' as const,
  letterSpacing: '0.05em',
  color: 'var(--text-muted)',
  borderBottom: '1px solid var(--border)',
  textAlign: 'left',
}