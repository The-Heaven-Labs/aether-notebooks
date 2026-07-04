import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { EmptyState } from '../components/EmptyState'
import { Zap } from 'lucide-react'
import { api } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import type { Skill } from '../types/agent'
import { PermissionsPanel } from '../components/PermissionsPanel'

interface SkillForm {
  name: string
  description: string
  system_prompt: string
}

const emptyForm = (): SkillForm => ({
  name: '',
  description: '',
  system_prompt: '',
})

export function SkillsPage() {
  useEffect(() => { document.title = "Skills — Aether Notebooks" }, [])

  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<SkillForm>(emptyForm())
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; name: string } | null>(null)
  const [permissionsTarget, setPermissionsTarget] = useState<{ id: string; name: string } | null>(null)

  const { data: skills = [], isLoading } = useQuery<Skill[]>({
    queryKey: ['skills'],
    queryFn: () => api.get<Skill[]>('/api/v1/skills'),
  })

  const createMutation = useMutation({
    mutationFn: () => api.post<{ id: string }>('/api/v1/skills', {
      name: form.name,
      description: form.description,
      system_prompt: form.system_prompt,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['skills'] })
      setCreating(false)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const updateMutation = useMutation({
    mutationFn: (id: string) => api.put<{ id: string }>(`/api/v1/skills/${id}`, {
      name: form.name,
      description: form.description,
      system_prompt: form.system_prompt,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['skills'] })
      setEditingId(null)
      setForm(emptyForm())
      setFormError(null)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/skills/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['skills'] }),
    onError: (e: unknown) => setDeleteError(String(e)),
  })

  const startEdit = (skill: Skill) => {
    setEditingId(skill.id)
    setForm({
      name: skill.name,
      description: skill.description ?? '',
      system_prompt: skill.system_prompt ?? '',
    })
  }

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader title="Skills" subtitle={skills.length > 0 ? `${skills.length} skill${skills.length !== 1 ? 's' : ''}` : ''}>
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Skill</button>
        </SectionHeader>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: -16, marginBottom: 24 }}>
          Reusable system prompt snippets that can be assigned to agents to specialize their behavior.
        </p>

        {creating && (
          <FormCard title="New Skill">
            <SkillFormFields form={form} setForm={setForm} />
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
          <FormCard title="Edit Skill">
            <SkillFormFields form={form} setForm={setForm} />
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

        {skills.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Zap size={28} />}
            title="No skills yet"
            text="Skills are reusable AI behaviors you can attach to agents to give them specialized capabilities."
            action={{ label: '+ New Skill', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Description', '']}>
            {skills.map((s) => (
              <tr key={s.id} style={rowStyle}>
                <td style={cellStyle}><strong>{s.name}</strong></td>
                <td style={cellStyle}><span style={{ color: 'var(--text-secondary)', fontSize: 13 }}>{s.description || '—'}</span></td>
                <td style={styles.tdActions}>
                  <button type="button" style={styles.permissionsBtn} onClick={() => setPermissionsTarget({ id: s.id, name: s.name })}>Permissions</button>
                  <button type="button" style={styles.editBtn} onClick={() => startEdit(s)}>Edit</button>
                  <button type="button" style={styles.deleteBtn} onClick={() => setDeleteTarget({ id: s.id, name: s.name })}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </StyledTable>
        )}
      </div>
      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete skill"
        message={`Delete "${deleteTarget?.name}"? This cannot be undone.`}
        confirmLabel="Delete"
        destructive
        onConfirm={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget.id); setDeleteTarget(null) }}
        onCancel={() => setDeleteTarget(null)}
      />
      {permissionsTarget && (
        <PermissionsPanel
          resourceType="skill"
          resourceId={permissionsTarget.id}
          resourceName={permissionsTarget.name}
          onClose={() => setPermissionsTarget(null)}
        />
      )}
    </AppShell>
  )
}

function SkillFormFields({ form, setForm }: {
  form: SkillForm
  setForm: React.Dispatch<React.SetStateAction<SkillForm>>
}) {
  return (
    <>
      <div style={styles.formGrid}>
        <label style={styles.label}>Name
          <input style={styles.input} value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} placeholder="Data Analyst" />
        </label>
        <label style={styles.label}>Description
          <input style={styles.input} value={form.description} onChange={e => setForm(f => ({ ...f, description: e.target.value }))} placeholder="Helps analyze data in notebooks" />
        </label>
        <label style={{ ...styles.label, gridColumn: '1 / -1' }}>System Prompt
          <textarea style={{ ...styles.input, minHeight: 100, resize: 'vertical' }} value={form.system_prompt} onChange={e => setForm(f => ({ ...f, system_prompt: e.target.value }))} placeholder="You are a data analyst expert. You help users explore their data, write queries, and create visualizations..." />
        </label>
      </div>
    </>
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
