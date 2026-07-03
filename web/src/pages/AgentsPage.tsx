import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { EmptyState } from '../components/EmptyState'
import { Bot } from 'lucide-react'
import { api, toolsApi } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import type { Agent, ModelConfig, Skill, MCPServerOrg, Tool } from '../types/agent'
import { PermissionsPanel } from '../components/PermissionsPanel'

interface AgentForm {
  name: string
  description: string
  system_prompt: string
  model_config_id: string
  subagent_model_config_id: string
  skill_ids: string[]
  tool_ids: string[]
  mcp_server_ids: string[]
  max_turns: number
}

const emptyForm = (): AgentForm => ({
  name: '',
  description: '',
  system_prompt: '',
  model_config_id: '',
  subagent_model_config_id: '',
  skill_ids: [],
  tool_ids: [],
  mcp_server_ids: [],
  max_turns: 90,
})

export function AgentsPage() {
  useEffect(() => { document.title = "Agents — Aether Notebooks" }, [])

  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<AgentForm>(emptyForm())
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; name: string } | null>(null)
  const [permissionsTarget, setPermissionsTarget] = useState<{ id: string; name: string } | null>(null)

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

  const { data: tools = [] } = useQuery<Tool[]>({
    queryKey: ['tools'],
    queryFn: toolsApi.list,
  })

  const createMutation = useMutation({
    mutationFn: () => api.post<{ id: string }>('/api/v1/agents', {
      name: form.name,
      description: form.description || undefined,
      system_prompt: form.system_prompt || undefined,
      model_config_id: form.model_config_id || undefined,
      subagent_model_config_id: form.subagent_model_config_id || undefined,
      skill_ids: form.skill_ids,
      tool_ids: form.tool_ids,
      mcp_server_ids: form.mcp_server_ids,
      max_turns: form.max_turns || undefined,
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
      tool_ids: form.tool_ids,
      mcp_server_ids: form.mcp_server_ids,
      max_turns: form.max_turns || undefined,
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
      tool_ids: agent.tool_ids ?? [],
      mcp_server_ids: agent.mcp_server_ids ?? [],
      max_turns: agent.max_turns ?? 90,
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

  const toggleTool = (id: string) => setForm(f => ({
    ...f,
    tool_ids: f.tool_ids.includes(id) ? f.tool_ids.filter(t => t !== id) : [...f.tool_ids, id],
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
              modelConfigs={modelConfigs} skills={skills} tools={tools} mcpServers={mcpServers}
              toggleSkill={toggleSkill} toggleTool={toggleTool} toggleMCPServer={toggleMCPServer}
            />
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setCreating(false); setForm(emptyForm()) }}>Cancel</button>
              <button type="button" style={styles.saveBtn} onClick={() => createMutation.mutate()} disabled={!form.name || !form.model_config_id || !form.subagent_model_config_id || createMutation.isPending}>
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
              modelConfigs={modelConfigs} skills={skills} tools={tools} mcpServers={mcpServers}
              toggleSkill={toggleSkill} toggleTool={toggleTool} toggleMCPServer={toggleMCPServer}
            />
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setEditingId(null); setForm(emptyForm()) }}>Cancel</button>
              <button type="button" style={styles.saveBtn} onClick={() => updateMutation.mutate(editingId!)} disabled={!form.name || !form.model_config_id || !form.subagent_model_config_id || updateMutation.isPending}>
                {updateMutation.isPending ? 'Saving...' : 'Save'}
              </button>
            </div>
            {formError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</p>}
          </FormCard>
        )}

        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}

        {agents.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Bot size={28} />}
            title="No agents yet"
            text="Agents are AI assistants that can run queries, analyze data, and build notebooks for you."
            action={{ label: '+ New Agent', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Model Config', 'Skills', 'Tools', 'MCP Servers', '']}>
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
                    {a.skills?.length
                      ? a.skills.map(s => s.name).join(', ')
                      : a.skill_ids?.length
                        ? a.skill_ids.map(id => skills.find(s => s.id === id)?.name ?? id.slice(0, 8)).join(', ')
                        : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                  </td>
                  <td style={cellStyle}>
                    {a.tools?.length
                      ? a.tools.map(t => t.name).join(', ')
                      : a.tool_ids?.length
                        ? a.tool_ids.map(id => tools.find(t => t.id === id)?.name ?? id.slice(0, 8)).join(', ')
                        : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                  </td>
                  <td style={cellStyle}>
                    {a.mcp_servers?.length
                      ? a.mcp_servers.map(m => m.name).join(', ')
                      : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                  </td>
                  <td style={styles.tdActions}>
                    <button type="button" style={styles.permissionsBtn} onClick={() => setPermissionsTarget({ id: a.id, name: a.name })}>Permissions</button>
                    <button type="button" style={styles.editBtn} onClick={() => startEdit(a)}>Edit</button>
                    <button type="button" style={styles.deleteBtn} onClick={() => setDeleteTarget({ id: a.id, name: a.name })}>
                      Delete
                    </button>
                  </td>
                </tr>
              )
            })}
          </StyledTable>
        )}
      </div>
      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete agent"
        message={`Delete "${deleteTarget?.name}"? This cannot be undone.`}
        confirmLabel="Delete"
        destructive
        onConfirm={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget.id); setDeleteTarget(null) }}
        onCancel={() => setDeleteTarget(null)}
      />
      {permissionsTarget && (
        <PermissionsPanel
          resourceType="agent"
          resourceId={permissionsTarget.id}
          resourceName={permissionsTarget.name}
          onClose={() => setPermissionsTarget(null)}
        />
      )}
    </AppShell>
  )
}

function AgentFormFields({ form, setForm, modelConfigs, skills, tools, mcpServers, toggleSkill, toggleTool, toggleMCPServer }: {
  form: AgentForm
  setForm: React.Dispatch<React.SetStateAction<AgentForm>>
  modelConfigs: ModelConfig[]
  skills: Skill[]
  tools: Tool[]
  mcpServers: MCPServerOrg[]
  toggleSkill: (id: string) => void
  toggleTool: (id: string) => void
  toggleMCPServer: (id: string) => void
}) {
  const [skillSearch, setSkillSearch] = useState('')
  const [toolSearch, setToolSearch] = useState('')
  const [mcpSearch, setMcpSearch] = useState('')

  const filteredSkills = skills.filter(s =>
    s.name.toLowerCase().includes(skillSearch.toLowerCase()) ||
    (s.description ?? '').toLowerCase().includes(skillSearch.toLowerCase())
  )
  const builtinTools = tools.filter(t => t.type === 'builtin')
  const userTools = tools.filter(t => t.type !== 'builtin').sort((a, b) => a.name.localeCompare(b.name))
  const builtinEnabled = builtinTools.length > 0 && builtinTools.every(t => form.tool_ids.includes(t.id))
  const toggleBuiltinAll = () => {
    const ids = builtinTools.map(t => t.id)
    const newIds = builtinEnabled
      ? form.tool_ids.filter(id => !ids.includes(id))
      : [...form.tool_ids, ...ids.filter(id => !form.tool_ids.includes(id))]
    setForm(f => ({ ...f, tool_ids: newIds }))
  }
  const filteredUserTools = userTools.filter(t => t.name.toLowerCase().includes(toolSearch.toLowerCase()))
  const filteredBuiltinTools = builtinTools.filter(t => t.name.toLowerCase().includes(toolSearch.toLowerCase()))
  const filteredMCPs = mcpServers.filter(m =>
    m.name.toLowerCase().includes(mcpSearch.toLowerCase())
  )

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
          {form.model_config_id === '' && <option value="">Select a model...</option>}
          {modelConfigs.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}
        </select>
      </label>
      <label style={styles.label}>Subagent Model
        <select style={styles.input} value={form.subagent_model_config_id} onChange={e => setForm(f => ({ ...f, subagent_model_config_id: e.target.value }))}>
          {form.subagent_model_config_id === '' && <option value="">Select a model...</option>}
          {modelConfigs.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}
        </select>
      </label>
      <label style={styles.label}>Max Tool Turns
        <input
          type="number"
          style={styles.input}
          value={form.max_turns}
          min={1}
          max={200}
          onChange={e => setForm(f => ({ ...f, max_turns: parseInt(e.target.value) || 90 }))}
        />
        <span style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 2 }}>Default: 90, Max: 200</span>
      </label>

      {/* Skills — searchable checkbox grid */}
      <div style={{ gridColumn: '1 / -1' }}>
        <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>Skills</div>
        <input
          style={styles.searchInput}
          placeholder="Search skills..."
          value={skillSearch}
          onChange={e => setSkillSearch(e.target.value)}
        />
        <div style={styles.selectorGrid}>
          {skills.length === 0 && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No skills yet</span>}
          {skills.length > 0 && filteredSkills.length === 0 && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No skills match "{skillSearch}"</span>}
          {filteredSkills.map(s => (
            <label key={s.id} style={styles.checkboxLabel}>
              <input
                type="checkbox"
                checked={form.skill_ids.includes(s.id)}
                onChange={() => toggleSkill(s.id)}
              />
              <span style={styles.checkboxText}>{s.name}</span>
            </label>
          ))}
        </div>
        {form.skill_ids.length > 0 && (
          <div style={styles.chipsRow}>
            {form.skill_ids.map(id => {
              const skill = skills.find(s => s.id === id)
              if (!skill) return null
              return (
                <span key={id} style={styles.chip}>
                  {skill.name}
                  <button type="button" onClick={() => toggleSkill(id)} style={styles.chipRemove}>×</button>
                </span>
              )
            })}
          </div>
        )}
      </div>

      {/* Tools — grouped by type */}
      <div style={{ gridColumn: '1 / -1' }}>
        <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>Tools</div>
        <input
          style={styles.searchInput}
          placeholder="Search tools..."
          value={toolSearch}
          onChange={e => setToolSearch(e.target.value)}
        />
        {tools.length === 0 && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No tools yet — <a href="/tools" style={{ color: 'var(--accent)' }}>create one</a></span>}

        {tools.length > 0 && (
          <>
            {/* Built-in tools: single toggle */}
            {filteredBuiltinTools.length > 0 && (
              <div style={{ marginBottom: 6 }}>
                <label style={{ ...styles.checkboxLabel, fontWeight: 600, color: 'var(--text-primary)' }}>
                  <input
                    type="checkbox"
                    checked={builtinEnabled}
                    onChange={toggleBuiltinAll}
                    ref={el => { if (el) el.indeterminate = builtinTools.some(t => form.tool_ids.includes(t.id)) && !builtinEnabled }}
                  />
                  <span>Built-in Tools ({builtinTools.length})</span>
                </label>
                {!toolSearch && (
                  <div style={{ fontSize: 10, color: 'var(--text-muted)', marginLeft: 22, marginTop: 2 }}>
                    {builtinTools.map(t => t.name).join(', ')}
                  </div>
                )}
              </div>
            )}

            {/* User-defined tools: individual checkboxes */}
            {filteredUserTools.length > 0 && (
              <div>
                <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4, marginTop: 4 }}>User-defined Tools</div>
                <div style={styles.selectorGrid}>
                  {filteredUserTools.map(t => (
                    <label key={t.id} style={styles.checkboxLabel}>
                      <input
                        type="checkbox"
                        checked={form.tool_ids.includes(t.id)}
                        onChange={() => toggleTool(t.id)}
                      />
                      <span style={styles.checkboxText}>{t.name} <span style={{ opacity: 0.6, fontSize: 10 }}>({t.type})</span></span>
                    </label>
                  ))}
                </div>
              </div>
            )}

            {filteredBuiltinTools.length === 0 && filteredUserTools.length === 0 && (
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No tools match "{toolSearch}"</span>
            )}
          </>
        )}

        {form.tool_ids.length > 0 && (
          <div style={styles.chipsRow}>
            {form.tool_ids.map(id => {
              const tool = tools.find(t => t.id === id)
              if (!tool) return null
              return (
                <span key={id} style={styles.chip}>
                  {tool.name}
                  <button type="button" onClick={() => toggleTool(id)} style={styles.chipRemove}>×</button>
                </span>
              )
            })}
          </div>
        )}
      </div>

      {/* MCP Servers — searchable checkbox grid */}
      <div style={{ gridColumn: '1 / -1' }}>
        <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>MCP Servers</div>
        <input
          style={styles.searchInput}
          placeholder="Search MCP servers..."
          value={mcpSearch}
          onChange={e => setMcpSearch(e.target.value)}
        />
        <div style={styles.selectorGrid}>
          {mcpServers.length === 0 && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No MCP servers configured — <a href="/mcps" style={{ color: 'var(--accent)' }}>create one</a></span>}
          {mcpServers.length > 0 && filteredMCPs.length === 0 && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No servers match "{mcpSearch}"</span>}
          {filteredMCPs.map(m => (
            <label key={m.id} style={styles.checkboxLabel}>
              <input
                type="checkbox"
                checked={form.mcp_server_ids.includes(m.id)}
                onChange={() => toggleMCPServer(m.id)}
              />
              <span style={styles.checkboxText}>{m.name} <span style={{ opacity: 0.6, fontSize: 10 }}>({m.type})</span></span>
            </label>
          ))}
        </div>
        {form.mcp_server_ids.length > 0 && (
          <div style={styles.chipsRow}>
            {form.mcp_server_ids.map(id => {
              const srv = mcpServers.find(m => m.id === id)
              if (!srv) return null
              return (
                <span key={id} style={styles.chip}>
                  {srv.name}
                  <button type="button" onClick={() => toggleMCPServer(id)} style={styles.chipRemove}>×</button>
                </span>
              )
            })}
          </div>
        )}
      </div>
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
  tdActions: { padding: '8px 16px', textAlign: 'right' as const },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--accent-light)', color: 'var(--text-secondary)', padding: '2px 7px', borderRadius: 3 },
  searchInput: { width: '100%', padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, background: 'var(--bg-input)', color: 'var(--text-primary)', outline: 'none', marginBottom: 6 },
  selectorGrid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 4, maxHeight: 200, overflowY: 'auto', border: '1px solid var(--border)', borderRadius: 4, padding: 8, background: 'var(--bg-input)' },
  checkboxLabel: { display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, cursor: 'pointer', padding: '3px 4px', borderRadius: 3 },
  checkboxText: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--text-primary)' },
  chipsRow: { display: 'flex', flexWrap: 'wrap', gap: 4, marginTop: 8 },
  chip: { display: 'inline-flex', alignItems: 'center', gap: 4, padding: '2px 8px', background: 'var(--accent-light)', border: '1px solid var(--accent)', borderRadius: 12, fontSize: 11, color: 'var(--text-primary)' },
  chipRemove: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', padding: 0, fontSize: 13, lineHeight: 1 },
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