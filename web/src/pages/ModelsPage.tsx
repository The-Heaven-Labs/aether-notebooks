import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { EmptyState } from '../components/EmptyState'
import { Brain } from 'lucide-react'
import { api } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import type { ModelConfig } from '../types/agent'

interface ModelConfigForm {
  name: string
  provider: string
  base_url: string
  model: string
  api_key: string
  context_window: number
}

const emptyForm = (): ModelConfigForm => ({
  name: '',
  provider: 'openai',
  base_url: 'https://api.openai.com/v1',
  model: 'gpt-4o',
  api_key: '',
  context_window: 128000,
})

const PROVIDERS = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'google', label: 'Google AI' },
  { value: 'opencode_zen', label: 'OpenCode Zen' },
  { value: 'opencode_go', label: 'OpenCode Go' },
  { value: 'openrouter', label: 'OpenRouter' },
  { value: 'ollama', label: 'Ollama (local)' },
  { value: 'lmstudio', label: 'LM Studio' },
  { value: 'together', label: 'Together AI' },
  { value: 'groq', label: 'Groq' },
  { value: 'fireworks', label: 'Fireworks AI' },
  { value: 'mistral', label: 'Mistral' },
  { value: 'deepseek', label: 'DeepSeek' },
  { value: 'other', label: 'Other (custom endpoint)' },
]

const PROVIDER_DEFAULTS: Record<string, { base_url: string; model: string }> = {
  openai:      { base_url: 'https://api.openai.com/v1',                model: 'gpt-4o' },
  anthropic:   { base_url: 'https://api.anthropic.com/v1',               model: 'claude-sonnet-4-20250514' },
  google:      { base_url: 'https://generativelanguage.googleapis.com/v1', model: 'gemini-2.0-flash' },
  opencode_zen: { base_url: 'https://api.opencode.ai/zen/v1',             model: '' },
  opencode_go:  { base_url: 'https://opencode.ai/zen/go/v1',              model: '' },
  openrouter:  { base_url: 'https://openrouter.ai/api/v1',               model: 'openai/gpt-4o' },
  ollama:      { base_url: 'http://localhost:11434/v1',                   model: 'llama3' },
  lmstudio:    { base_url: 'http://localhost:1234/v1',                   model: 'llama3' },
  together:    { base_url: 'https://api.together.xyz/v1',                model: 'mistralai/Mistral-7B-Instruct-v0.2' },
  groq:        { base_url: 'https://api.groq.com/openai/v1',            model: 'llama-3.1-70b-versatile' },
  fireworks:   { base_url: 'https://api.fireworks.ai/inference/v1',         model: 'accounts/fireworks/models/llama-v3-70b-instruct' },
  mistral:     { base_url: 'https://api.mistral.ai/v1',                   model: 'mistral-small-latest' },
  deepseek:    { base_url: 'https://api.deepseek.com/v1',                model: 'deepseek-chat' },
  other:       { base_url: '',                                          model: '' },
}

export function ModelsPage() {
  useEffect(() => { document.title = "Models — Heaven's Notebooks" }, [])

  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<ModelConfigForm>(emptyForm())
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const { data: configs = [], isLoading } = useQuery<ModelConfig[]>({
    queryKey: ['model-configs'],
    queryFn: () => api.get<ModelConfig[]>('/api/v1/model-configs'),
  })

  const createMutation = useMutation({
    mutationFn: () => api.post<{ id: string }>('/api/v1/model-configs', {
      name: form.name,
      provider: form.provider,
      base_url: form.base_url,
      model: form.model,
      api_key: form.api_key,
      context_window: form.context_window,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['model-configs'] })
      setCreating(false)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const updateMutation = useMutation({
    mutationFn: (id: string) => api.put<{ id: string }>(`/api/v1/model-configs/${id}`, {
      name: form.name,
      provider: form.provider,
      base_url: form.base_url,
      model: form.model,
      ...(form.api_key ? { api_key: form.api_key } : {}),
      context_window: form.context_window,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['model-configs'] })
      setEditingId(null)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/model-configs/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['model-configs'] }),
    onError: (e: unknown) => setDeleteError(String(e)),
  })

  const [testResult, setTestResult] = useState<{ id: string; ok: boolean; message: string } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ModelConfig | null>(null)
  const testMutation = useMutation({
    mutationFn: async (id: string) => {
      const res = await api.post<{ status: string; response: string; model: string }>(`/api/v1/model-configs/${id}/test`, {})
      return res
    },
    onSuccess: (data, id) => {
      setTestResult({ id, ok: true, message: `OK — "${data.response}" (model: ${data.model})` })
    },
    onError: (e: unknown, id) => {
      setTestResult({ id, ok: false, message: String(e) })
    },
  })

  const startEdit = (config: ModelConfig) => {
    setEditingId(config.id)
    setForm({
      name: config.name,
      provider: config.provider,
      base_url: config.base_url,
      model: config.model,
      api_key: '',
      context_window: config.context_window,
    })
  }

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader title="Models" subtitle={configs.length > 0 ? `${configs.length} model config${configs.length !== 1 ? 's' : ''}` : ''}>
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Model</button>
        </SectionHeader>

        {creating && (
          <FormCard title="New Model">
            <ModelFormFields form={form} setForm={setForm} />
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setCreating(false); setForm(emptyForm()) }}>Cancel</button>
              <button type="button" style={styles.saveBtn} onClick={() => createMutation.mutate()} disabled={!form.name || !form.model || createMutation.isPending}>
                {createMutation.isPending ? 'Creating…' : 'Create'}
              </button>
            </div>
            {formError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</p>}
          </FormCard>
        )}

        {editingId && (
          <FormCard title="Edit Model">
            <ModelFormFields form={form} setForm={setForm} />
            <div style={styles.formActions}>
              <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>(leave API key blank to keep current)</span>
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

        {configs.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Brain size={28} />}
            title="No model configs yet"
            text="Add a model configuration to connect AI providers for your agents."
            action={{ label: '+ New Model', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Provider', 'Endpoint', 'Model', 'Context Window', '']}>
            {configs.map((c) => (
              <tr key={c.id} style={rowStyle}>
                <td style={cellStyle}><strong>{c.name}</strong></td>
                <td style={cellStyle}><code style={styles.badge}>{c.provider}</code></td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--text-secondary)' }}>{c.base_url}</td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{c.model}</td>
                <td style={{ ...cellStyle, fontSize: 12, color: 'var(--text-muted)' }}>{c.context_window?.toLocaleString() ?? '—'}</td>
                <td style={styles.tdActions}>
                  <button
                    type="button"
                    style={testMutation.isPending && testResult?.id === c.id ? { ...styles.testBtn, opacity: 0.6 } : styles.testBtn}
                    onClick={() => { setTestResult(null); testMutation.mutate(c.id) }}
                    disabled={testMutation.isPending}
                  >
                    {testMutation.isPending && testResult?.id === c.id ? 'Testing…' : 'Test'}
                  </button>
                  <button type="button" style={styles.editBtn} onClick={() => startEdit(c)}>Edit</button>
                  <button type="button" style={styles.deleteBtn} onClick={() => setDeleteTarget(c)}>
                    Delete
                  </button>
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
            {testResult.ok ? '✅ Connection OK: ' : '❌ Connection failed: '}{testResult.message}
          </div>
        )}
      </div>
      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete model config"
        message={`Delete "${deleteTarget?.name}"? This cannot be undone.`}
        confirmLabel="Delete"
        destructive
        onConfirm={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget.id); setDeleteTarget(null) }}
        onCancel={() => setDeleteTarget(null)}
      />
    </AppShell>
  )
}

function ModelFormFields({ form, setForm }: { form: ModelConfigForm; setForm: React.Dispatch<React.SetStateAction<ModelConfigForm>> }) {
  const setField = (field: keyof ModelConfigForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm(f => ({ ...f, [field]: e.target.value }))

  return (
    <div style={styles.formGrid}>
      <label style={styles.label}>Name
        <input style={styles.input} value={form.name} onChange={setField('name')} placeholder="GPT-4o Production" />
      </label>
      <label style={styles.label}>Provider
        <select style={styles.input} value={form.provider} onChange={e => {
          const prov = e.target.value
          const defaults = PROVIDER_DEFAULTS[prov] || {}
          setForm(f => ({
            ...f,
            provider: prov,
            base_url: defaults.base_url ?? f.base_url,
            model: defaults.model ?? f.model,
          }))
        }}>
          {PROVIDERS.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
        </select>
      </label>
      <label style={styles.label}>Base URL
        <input style={styles.input} value={form.base_url} onChange={setField('base_url')} placeholder="https://api.openai.com/v1" />
      </label>
      <label style={styles.label}>Model
        <input style={styles.input} value={form.model} onChange={setField('model')} placeholder="gpt-4o" />
      </label>
      <label style={styles.label}>API Key
        <input style={styles.input} type="password" value={form.api_key} onChange={setField('api_key')} placeholder="sk-..." />
      </label>
      <label style={styles.label}>Context Window (tokens)
        <input style={styles.input} type="number" min={1000} max={2000000} value={form.context_window} onChange={e => setForm(f => ({ ...f, context_window: parseInt(e.target.value) || 128000 }))} />
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
  testBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 },
  tdActions: { padding: '8px 16px', textAlign: 'right' as const },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--accent-light)', color: 'var(--text-secondary)', padding: '2px 7px', borderRadius: 3 },
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
