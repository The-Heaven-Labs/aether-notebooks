import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { EmptyState } from '../components/EmptyState'
import { StatusBadge } from '../components/StatusBadge'
import { Brain, Check, Pencil, ShieldCheck, Trash2, X, Zap } from 'lucide-react'
import { api } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { FormModal } from '../components/FormModal'
import { RowAction, RowActionsBreak, RowActionsCell, RowActionsHeader } from '../components/RowActions'
import type { ModelConfig } from '../types/agent'
import { PermissionsPanel } from '../components/PermissionsPanel'

interface ModelConfigForm {
  name: string
  provider: string
  base_url: string
  model: string
  api_key: string
  context_window: number
  compaction_threshold: number
  reasoning_effort_options: string
  reasoning_effort: string
  price_per_input_token: string
  price_per_output_token: string
  price_per_cache_read_token: string
}

const emptyForm = (): ModelConfigForm => ({
  name: '',
  provider: 'openai',
  base_url: 'https://api.openai.com/v1',
  model: 'gpt-4o',
  api_key: '',
  context_window: 128000,
  compaction_threshold: 70,
  reasoning_effort_options: 'low, medium, high',
  reasoning_effort: '',
  price_per_input_token: '0',
  price_per_output_token: '0',
  price_per_cache_read_token: '0',
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
  useEffect(() => { document.title = "Models — Aether Notebooks" }, [])

  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<ModelConfigForm>(emptyForm())
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const nameRef = useRef<HTMLInputElement>(null)

  const { data: configs = [], isLoading } = useQuery<ModelConfig[]>({
    queryKey: ['model-configs'],
    queryFn: () => api.get<ModelConfig[]>('/api/v1/model-configs'),
  })

  const defaultParams = () => {
    const p: Record<string, unknown> = { compaction_threshold: form.compaction_threshold }
    if (form.reasoning_effort_options) {
      p['reasoning_effort_options'] = form.reasoning_effort_options.split(',').map(s => s.trim()).filter(Boolean)
    }
    if (form.reasoning_effort) p['reasoning_effort'] = form.reasoning_effort
    return p
  }

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
    mutationFn: () => api.post<{ id: string }>('/api/v1/model-configs', {
      name: form.name,
      provider: form.provider,
      base_url: form.base_url,
      model: form.model,
      api_key: form.api_key,
      context_window: form.context_window,
      default_params: defaultParams(),
      price_per_input_token: parseFloat(form.price_per_input_token) || 0,
      price_per_output_token: parseFloat(form.price_per_output_token) || 0,
      price_per_cache_read_token: parseFloat(form.price_per_cache_read_token) || 0,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['model-configs'] })
      closeCreate()
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
      default_params: defaultParams(),
      price_per_input_token: parseFloat(form.price_per_input_token) || 0,
      price_per_output_token: parseFloat(form.price_per_output_token) || 0,
      price_per_cache_read_token: parseFloat(form.price_per_cache_read_token) || 0,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['model-configs'] })
      closeEdit()
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
  const [permissionsTarget, setPermissionsTarget] = useState<{ id: string; name: string } | null>(null)
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
    const threshold = config.default_params?.['compaction_threshold']
    const effortOpts = config.default_params?.['reasoning_effort_options']
    const effort = config.default_params?.['reasoning_effort']
    setForm({
      name: config.name,
      provider: config.provider,
      base_url: config.base_url,
      model: config.model,
      api_key: '',
      context_window: config.context_window,
      compaction_threshold: typeof threshold === 'number' ? threshold : 70,
      reasoning_effort_options: Array.isArray(effortOpts) ? effortOpts.join(', ') : 'low, medium, high',
      reasoning_effort: typeof effort === 'string' ? effort : '',
      price_per_input_token: String(config.price_per_input_token ?? 0),
      price_per_output_token: String(config.price_per_output_token ?? 0),
      price_per_cache_read_token: String(config.price_per_cache_read_token ?? 0),
    })
    setEditingId(config.id)
    setFormError(null)
  }

  const editingConfig = configs.find((c) => c.id === editingId)

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader title="Models" subtitle={configs.length > 0 ? `${configs.length} model config${configs.length !== 1 ? 's' : ''}` : ''}>
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Model</button>
        </SectionHeader>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: -16, marginBottom: 24 }}>
          Configure LLM provider connections (API keys, models, pricing) for agents to use.
        </p>

        {creating && (
          <FormModal
            title="New Model"
            onClose={closeCreate}
            initialFocusRef={nameRef}
            error={formError}
            submitLabel="Create"
            pendingLabel="Creating…"
            pending={createMutation.isPending}
            submitDisabled={!form.name || !form.model}
            submitTitle={!form.name ? 'Name is required' : !form.model ? 'Model is required' : undefined}
            onSubmit={() => createMutation.mutate()}
          >
            <ModelFormFields form={form} setForm={setForm} nameRef={nameRef} />
          </FormModal>
        )}

        {editingId && (
          <FormModal
            title={`Edit "${editingConfig?.name ?? 'model'}"`}
            onClose={closeEdit}
            initialFocusRef={nameRef}
            error={formError}
            footerExtra={<span style={{ fontSize: 12, color: 'var(--text-muted)' }}>(leave API key blank to keep current)</span>}
            submitLabel="Save"
            pendingLabel="Saving…"
            pending={updateMutation.isPending}
            submitDisabled={!form.name}
            submitTitle={!form.name ? 'Name is required' : undefined}
            onSubmit={() => updateMutation.mutate(editingId!)}
          >
            <ModelFormFields form={form} setForm={setForm} nameRef={nameRef} />
          </FormModal>
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
          <StyledTable
            headers={['Name', 'Provider', 'Endpoint', 'Model', 'Context Window', 'Compaction', 'Status', <RowActionsHeader />]}
            headerClassNames={[undefined, undefined, undefined, undefined, undefined, undefined, undefined, 'row-actions-header']}
          >
            {configs.map((c) => {
              const testing = testMutation.isPending && testMutation.variables === c.id
              const test = testResult?.id === c.id ? testResult : null
              return (
                <tr key={c.id} style={rowStyle}>
                  <td style={cellStyle}><strong>{c.name}</strong></td>
                  <td style={cellStyle}><code style={styles.badge}>{c.provider}</code></td>
                  <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--text-secondary)' }}>{c.base_url}</td>
                  <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{c.model}</td>
                  <td style={{ ...cellStyle, fontSize: 12, color: 'var(--text-muted)' }}>{c.context_window?.toLocaleString() ?? '—'}</td>
                  <td style={{ ...cellStyle, fontSize: 12, color: 'var(--text-muted)' }}>
                    {c.default_params?.['compaction_threshold'] != null ? `${c.default_params['compaction_threshold']}%` : '70%'}
                  </td>
                  <td style={cellStyle}>
                    {testing ? (
                      <StatusBadge status="neutral" label="Testing…" />
                    ) : test ? (
                      <StatusBadge
                        status={test.ok ? 'success' : 'error'}
                        label={test.ok ? 'Connected' : 'Failed'}
                        icon={test.ok ? <Check size={12} /> : <X size={12} />}
                        title={test.message}
                      />
                    ) : (
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>
                        Unknown — click Test
                      </span>
                    )}
                  </td>
                  <RowActionsCell>
                    <RowAction
                      label="Test model config"
                      icon={<Zap size={13} />}
                      spinning={testing}
                      disabled={testMutation.isPending}
                      onClick={() => { setTestResult(null); testMutation.mutate(c.id) }}
                    />
                    <RowAction label="Edit model config" icon={<Pencil size={13} />} accent onClick={() => startEdit(c)} />
                    <RowActionsBreak />
                    <RowAction label="Permissions" icon={<ShieldCheck size={13} />} onClick={() => setPermissionsTarget({ id: c.id, name: c.name })} />
                    <RowAction label="Delete model config" icon={<Trash2 size={13} />} danger onClick={() => setDeleteTarget(c)} />
                  </RowActionsCell>
                </tr>
              )
            })}
          </StyledTable>
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
      {permissionsTarget && (
        <PermissionsPanel
          resourceType="model_config"
          resourceId={permissionsTarget.id}
          resourceName={permissionsTarget.name}
          onClose={() => setPermissionsTarget(null)}
        />
      )}
    </AppShell>
  )
}

function ModelFormFields({ form, setForm, nameRef }: {
  form: ModelConfigForm
  setForm: React.Dispatch<React.SetStateAction<ModelConfigForm>>
  nameRef: React.RefObject<HTMLInputElement | null>
}) {
  const setField = (field: keyof ModelConfigForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm(f => ({ ...f, [field]: e.target.value }))

  return (
    <>
      <label style={styles.label}>Name
        <input ref={nameRef} style={styles.input} value={form.name} onChange={setField('name')} placeholder="GPT-4o Production" />
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
        <input style={styles.input} type="text" value={form.context_window} onChange={e => setForm(f => ({ ...f, context_window: parseInt(e.target.value) || 128000 }))} />
      </label>
      <label style={styles.label}>Compaction Threshold %
        <input style={styles.input} type="text" value={form.compaction_threshold} onChange={e => setForm(f => ({ ...f, compaction_threshold: parseInt(e.target.value) || 70 }))} />
        <span style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 400 }}>Auto-summarize when context reaches this % of the window. 0 = disabled.</span>
      </label>
      <label style={styles.label}>Reasoning Effort Options
        <input style={styles.input} value={form.reasoning_effort_options} onChange={e => setForm(f => ({ ...f, reasoning_effort_options: e.target.value }))} placeholder="low, medium, high" />
        <span style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 400 }}>Comma-separated effort levels users can pick from the chat.</span>
      </label>
      <label style={styles.label}>Default Effort
        <select style={styles.input} value={form.reasoning_effort} onChange={e => setForm(f => ({ ...f, reasoning_effort: e.target.value }))}>
          <option value="">None</option>
          {form.reasoning_effort_options.split(',').map(s => s.trim()).filter(Boolean).map(o => (
            <option key={o} value={o}>{o}</option>
          ))}
        </select>
        <span style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 400 }}>Default effort level pre-selected in the chat.</span>
      </label>
      <label style={styles.label}>Input Token Price ($ per 1M tokens)
        <input style={styles.input} type="text" value={form.price_per_input_token} onChange={e => setForm(f => ({ ...f, price_per_input_token: e.target.value }))} placeholder="0.15" />
        <span style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 400 }}>Cost per 1M input tokens (e.g. 0.15 for GPT-4o-mini, 2.50 for GPT-4o).</span>
      </label>
      <label style={styles.label}>Output Token Price ($ per 1M tokens)
        <input style={styles.input} type="text" value={form.price_per_output_token} onChange={e => setForm(f => ({ ...f, price_per_output_token: e.target.value }))} placeholder="0.60" />
        <span style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 400 }}>Cost per 1M output tokens (e.g. 0.60 for GPT-4o-mini, 10.00 for GPT-4o).</span>
      </label>
      <label style={styles.label}>Cache Read Token Price ($ per 1M tokens)
        <input style={styles.input} type="text" value={form.price_per_cache_read_token} onChange={e => setForm(f => ({ ...f, price_per_cache_read_token: e.target.value }))} placeholder="0.075" />
        <span style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 400 }}>Cost per 1M cached input tokens (e.g. 0.075 for GPT-4o-mini).</span>
      </label>
    </>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2, appearance: 'none', WebkitAppearance: 'none', MozAppearance: 'none' },
  newBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--accent-light)', color: 'var(--text-secondary)', padding: '2px 7px', borderRadius: 3 },
}
