import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { EmptyState } from '../components/EmptyState'
import { Zap, Pencil, ShieldCheck, Trash2 } from 'lucide-react'
import { api } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { FormModal } from '../components/FormModal'
import { RowAction, RowActionsBreak, RowActionsCell, RowActionsHeader } from '../components/RowActions'
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
  const nameRef = useRef<HTMLInputElement>(null)

  const { data: skills = [], isLoading } = useQuery<Skill[]>({
    queryKey: ['skills'],
    queryFn: () => api.get<Skill[]>('/api/v1/skills'),
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
    mutationFn: () => api.post<{ id: string }>('/api/v1/skills', {
      name: form.name,
      description: form.description,
      system_prompt: form.system_prompt,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['skills'] })
      closeCreate()
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
      closeEdit()
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
    setFormError(null)
    setForm({
      name: skill.name,
      description: skill.description ?? '',
      system_prompt: skill.system_prompt ?? '',
    })
  }

  const editingSkill = skills.find((s) => s.id === editingId)

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
          <FormModal
            title="New Skill"
            onClose={closeCreate}
            initialFocusRef={nameRef}
            error={formError}
            submitLabel="Create"
            pendingLabel="Creating…"
            pending={createMutation.isPending}
            submitDisabled={!form.name}
            submitTitle={!form.name ? 'Name is required' : undefined}
            onSubmit={() => createMutation.mutate()}
          >
            <SkillFormFields form={form} setForm={setForm} nameRef={nameRef} />
          </FormModal>
        )}

        {editingId && (
          <FormModal
            title={`Edit "${editingSkill?.name ?? 'skill'}"`}
            onClose={closeEdit}
            initialFocusRef={nameRef}
            error={formError}
            submitLabel="Save"
            pendingLabel="Saving…"
            pending={updateMutation.isPending}
            submitDisabled={!form.name}
            submitTitle={!form.name ? 'Name is required' : undefined}
            onSubmit={() => updateMutation.mutate(editingId!)}
          >
            <SkillFormFields form={form} setForm={setForm} nameRef={nameRef} />
          </FormModal>
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
          <StyledTable
            headers={['Name', 'Description', <RowActionsHeader />]}
            headerClassNames={[undefined, undefined, 'row-actions-header']}
          >
            {skills.map((s) => (
              <tr key={s.id} style={rowStyle}>
                <td style={cellStyle}><strong>{s.name}</strong></td>
                <td style={cellStyle}><span style={{ color: 'var(--text-secondary)', fontSize: 13 }}>{s.description || '—'}</span></td>
                <RowActionsCell>
                  <RowAction label="Edit skill" icon={<Pencil size={13} />} accent onClick={() => startEdit(s)} />
                  <RowActionsBreak />
                  <RowAction label="Permissions" icon={<ShieldCheck size={13} />} onClick={() => setPermissionsTarget({ id: s.id, name: s.name })} />
                  <RowAction label="Delete skill" icon={<Trash2 size={13} />} danger onClick={() => setDeleteTarget({ id: s.id, name: s.name })} />
                </RowActionsCell>
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

function SkillFormFields({ form, setForm, nameRef }: {
  form: SkillForm
  setForm: React.Dispatch<React.SetStateAction<SkillForm>>
  nameRef: React.RefObject<HTMLInputElement | null>
}) {
  return (
    <>
      <label style={styles.label}>Name
        <input ref={nameRef} style={styles.input} value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} placeholder="Data Analyst" />
      </label>
      <label style={styles.label}>Description
        <input style={styles.input} value={form.description} onChange={e => setForm(f => ({ ...f, description: e.target.value }))} placeholder="Helps analyze data in notebooks" />
      </label>
      <label style={{ ...styles.label, gridColumn: '1 / -1' }}>System Prompt
        <textarea style={{ ...styles.input, minHeight: 100, resize: 'vertical' }} value={form.system_prompt} onChange={e => setForm(f => ({ ...f, system_prompt: e.target.value }))} placeholder="You are a data analyst expert. You help users explore their data, write queries, and create visualizations..." />
      </label>
    </>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2 },
  newBtn: { padding: '7px 16px', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
}
