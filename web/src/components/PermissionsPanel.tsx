import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'

// ─── Types ───────────────────────────────────────────────────────────────────

type ResourceType = 'folder' | 'notebook' | 'connector' | 'dashboard'

const ACTION_LABELS: Record<ResourceType, string[]> = {
  folder:    ['view', 'create', 'edit', 'manage', 'delete'],
  notebook:  ['view', 'run', 'edit', 'share', 'delete'],
  connector: ['view', 'use', 'edit', 'share', 'delete'],
  dashboard: ['view', 'edit', 'share', 'delete'],
}

interface AclEntry {
  id: string
  subject_type: 'user' | 'group'
  subject_id: string
  actions: string[]
}

interface Member {
  id: string
  name: string
  email: string
  role: string
}

interface Group {
  id: string
  name: string
}

export interface PermissionsPanelProps {
  resourceType: ResourceType
  resourceId: string
  resourceName: string
  parentFolderId?: string
  onClose: () => void
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function initials(name: string): string {
  return name
    .split(' ')
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0].toUpperCase())
    .join('')
}

function Avatar({ name, type }: { name: string; type: 'user' | 'group' }) {
  return (
    <div style={styles.avatar}>
      {type === 'group' ? '#' : initials(name)}
    </div>
  )
}

// ─── Component ───────────────────────────────────────────────────────────────

export function PermissionsPanel({
  resourceType,
  resourceId,
  resourceName,
  parentFolderId,
  onClose,
}: PermissionsPanelProps) {
  const qc = useQueryClient()
  const actions = ACTION_LABELS[resourceType]

  // Draft state for unsaved ACL changes
  const [draft, setDraft] = useState<AclEntry[] | null>(null)

  // Reset draft when resource changes
  useEffect(() => {
    setDraft(null)
  }, [resourceId])

  // Local draft for new entry
  const [newSubjectKey, setNewSubjectKey] = useState<string>('') // "{type}:{id}"
  const [newActions, setNewActions] = useState<string[]>([])
  const [saveError, setSaveError] = useState<string | null>(null)

  // ── Queries ──

  const aclKey = ['acl', resourceType, resourceId]

  const { data: aclData, isLoading: aclLoading } = useQuery<AclEntry[]>({
    queryKey: aclKey,
    queryFn: () => api.get<AclEntry[]>(`/api/v1/acl/${resourceType}/${resourceId}`),
  })

  const { data: members = [] } = useQuery<Member[]>({
    queryKey: ['members'],
    queryFn: () => api.get<Member[]>('/api/v1/members'),
  })

  const { data: groups = [] } = useQuery<Group[]>({
    queryKey: ['groups'],
    queryFn: () => api.get<Group[]>('/api/v1/groups'),
  })

  const { data: parentAcl } = useQuery<AclEntry[]>({
    queryKey: ['acl', 'folder', parentFolderId],
    queryFn: () => api.get<AclEntry[]>(`/api/v1/acl/folder/${parentFolderId}`),
    enabled: !!parentFolderId,
  })

  // ── Mutation ──

  const saveAcl = useMutation({
    mutationFn: (entries: Omit<AclEntry, 'id'>[]) =>
      api.put<AclEntry[]>(`/api/v1/acl/${resourceType}/${resourceId}`, { entries }),
    onSuccess: () => {
      setSaveError(null)
      setDraft(null)
      qc.invalidateQueries({ queryKey: aclKey })
    },
    onError: (err: unknown) => {
      setSaveError(err instanceof Error ? err.message : 'Failed to save permissions')
    },
  })

  // ── Helpers ──

  function subjectName(entry: AclEntry): string {
    if (entry.subject_type === 'user') {
      const m = members.find((m) => m.id === entry.subject_id)
      return m ? m.name || m.email : entry.subject_id
    } else {
      const g = groups.find((g) => g.id === entry.subject_id)
      return g ? g.name : entry.subject_id
    }
  }

  function handleToggleAction(entryIndex: number, action: string) {
    const current = draft ?? aclData ?? []
    const updated = current.map((e, i) => {
      if (i !== entryIndex) return e
      const actions = e.actions.includes(action)
        ? e.actions.filter(a => a !== action)
        : [...e.actions, action]
      return { ...e, actions }
    })
    setDraft(updated)
  }

  function handleRemoveEntry(entryIndex: number) {
    const current = draft ?? aclData ?? []
    setDraft(current.filter((_, i) => i !== entryIndex))
  }

  function handleAddEntry() {
    if (!newSubjectKey || newActions.length === 0) return
    const [subjectType, subjectId] = newSubjectKey.split(':') as ['user' | 'group', string]
    const current = draft ?? aclData ?? []
    const updated: AclEntry[] = [
      ...current,
      { id: '', subject_type: subjectType, subject_id: subjectId, actions: newActions },
    ]
    setDraft(updated)
    setNewSubjectKey('')
    setNewActions([])
  }

  function toggleNewAction(action: string) {
    setNewActions((prev) =>
      prev.includes(action) ? prev.filter((a) => a !== action) : [...prev, action]
    )
  }

  // ── Derived ──

  const inheritedCount = parentAcl?.length ?? 0

  const typeBadgeColors: Record<ResourceType, string> = {
    folder: '#e8f0fe',
    notebook: '#fce8ff',
    connector: '#e8fff0',
    dashboard: '#fff8e8',
  }

  // ── Render ──

  return (
    <>
      {/* Backdrop */}
      <div style={styles.backdrop} onClick={onClose} />

      {/* Drawer */}
      <div style={styles.drawer}>
        {/* Header */}
        <div style={styles.header}>
          <div style={styles.headerLeft}>
            <span style={styles.resourceName}>{resourceName}</span>
            <span style={{ ...styles.typeBadge, background: typeBadgeColors[resourceType] }}>
              {resourceType}
            </span>
          </div>
          <button style={styles.closeBtn} onClick={onClose} title="Close">×</button>
        </div>

        {/* Inheritance note */}
        <div style={styles.inheritNote}>
          {parentFolderId
            ? inheritedCount > 0
              ? `Inheriting ${inheritedCount} permission${inheritedCount === 1 ? '' : 's'} from parent folder`
              : 'Inheriting 0 permissions from parent folder'
            : 'No inherited permissions'}
        </div>

        {/* Body */}
        <div style={styles.body}>
          {saveError && <div style={styles.errorText}>{saveError}</div>}

          {aclLoading ? (
            <div style={styles.loading}>Loading…</div>
          ) : (
            <>
              {/* ACL entries */}
              {(draft ?? aclData ?? []).length === 0 && (
                <div style={styles.emptyText}>No permissions set. Add one below.</div>
              )}

              {(draft ?? aclData ?? []).map((entry, idx) => (
                <div key={entry.id || idx} style={styles.entryRow}>
                  <Avatar name={subjectName(entry)} type={entry.subject_type} />
                  <div style={styles.entryInfo}>
                    <span style={styles.entryName}>{subjectName(entry)}</span>
                    <span style={styles.entryType}>{entry.subject_type}</span>
                  </div>
                  <div style={styles.checkboxGroup}>
                    {actions.map((action) => (
                      <label key={action} style={styles.checkLabel} title={action}>
                        <input
                          type="checkbox"
                          checked={entry.actions.includes(action)}
                          onChange={() => handleToggleAction(idx, action)}
                          style={{ marginRight: 3 }}
                        />
                        <span style={styles.actionLabel}>{action}</span>
                      </label>
                    ))}
                  </div>
                  <button
                    style={styles.removeBtn}
                    title="Remove"
                    onClick={() => handleRemoveEntry(idx)}
                  >
                    ×
                  </button>
                </div>
              ))}

              {/* Save / Discard */}
              {draft !== null && (
                <div style={styles.draftActions}>
                  <button
                    style={{
                      ...styles.saveBtn,
                      opacity: saveAcl.isPending ? 0.6 : 1,
                    }}
                    disabled={saveAcl.isPending}
                    onClick={() => saveAcl.mutate(draft.map(({ id: _id, ...rest }) => rest))}
                  >
                    {saveAcl.isPending ? 'Saving…' : 'Save'}
                  </button>
                  <button
                    style={styles.discardBtn}
                    disabled={saveAcl.isPending}
                    onClick={() => setDraft(null)}
                  >
                    Discard
                  </button>
                </div>
              )}

              {/* Divider */}
              <div style={styles.divider} />

              {/* Add entry row */}
              <div style={styles.addRow}>
                <select
                  style={styles.select}
                  value={newSubjectKey}
                  onChange={(e) => setNewSubjectKey(e.target.value)}
                >
                  <option value="">Select user or group…</option>
                  {members.length > 0 && (
                    <optgroup label="Users">
                      {members.map((m) => (
                        <option key={m.id} value={`user:${m.id}`}>
                          {m.name || m.email}
                        </option>
                      ))}
                    </optgroup>
                  )}
                  {groups.length > 0 && (
                    <optgroup label="Groups">
                      {groups.map((g) => (
                        <option key={g.id} value={`group:${g.id}`}>
                          {g.name}
                        </option>
                      ))}
                    </optgroup>
                  )}
                </select>

                <div style={styles.checkboxGroup}>
                  {actions.map((action) => (
                    <label key={action} style={styles.checkLabel} title={action}>
                      <input
                        type="checkbox"
                        checked={newActions.includes(action)}
                        onChange={() => toggleNewAction(action)}
                        style={{ marginRight: 3 }}
                      />
                      <span style={styles.actionLabel}>{action}</span>
                    </label>
                  ))}
                </div>

                <button
                  style={{
                    ...styles.addBtn,
                    opacity: !newSubjectKey || newActions.length === 0 || saveAcl.isPending ? 0.5 : 1,
                  }}
                  disabled={!newSubjectKey || newActions.length === 0 || saveAcl.isPending}
                  onClick={handleAddEntry}
                >
                  Add
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </>
  )
}

// ─── Styles ──────────────────────────────────────────────────────────────────

const styles: Record<string, React.CSSProperties> = {
  backdrop: {
    position: 'fixed',
    inset: 0,
    background: 'rgba(0,0,0,0.3)',
    zIndex: 1500,
  },
  drawer: {
    position: 'fixed',
    right: 0,
    top: 0,
    height: '100vh',
    width: 420,
    background: '#fff',
    boxShadow: '-4px 0 24px rgba(0,0,0,0.12)',
    zIndex: 1501,
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 20px',
    borderBottom: '1px solid var(--nav-border, #e8e8e8)',
    flexShrink: 0,
  },
  headerLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    minWidth: 0,
  },
  resourceName: {
    fontSize: 15,
    fontWeight: 700,
    color: '#111',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  typeBadge: {
    fontSize: 10,
    fontWeight: 700,
    borderRadius: 4,
    padding: '2px 7px',
    letterSpacing: '0.05em',
    textTransform: 'uppercase' as const,
    color: '#555',
    flexShrink: 0,
  },
  closeBtn: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    fontSize: 22,
    color: 'var(--text-muted, #888)',
    lineHeight: 1,
    padding: '0 4px',
    flexShrink: 0,
  },
  inheritNote: {
    fontSize: 12,
    color: 'var(--text-muted, #888)',
    padding: '8px 20px',
    borderBottom: '1px solid var(--nav-border, #e8e8e8)',
    background: '#fafafa',
    flexShrink: 0,
  },
  body: {
    flex: 1,
    overflowY: 'auto',
    padding: '16px 20px',
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  loading: {
    fontSize: 13,
    color: 'var(--text-muted, #888)',
    padding: '8px 0',
  },
  emptyText: {
    fontSize: 13,
    color: 'var(--text-muted, #888)',
    padding: '8px 0',
  },
  errorText: {
    fontSize: 12,
    color: '#d32f2f',
    background: '#fff0f0',
    border: '1px solid #fcc',
    borderRadius: 4,
    padding: '8px 12px',
  },
  entryRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '10px 0',
    borderBottom: '1px solid var(--nav-border, #e8e8e8)',
  },
  avatar: {
    width: 32,
    height: 32,
    borderRadius: '50%',
    background: 'var(--accent, #5c6bc0)',
    color: '#fff',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 11,
    fontWeight: 700,
    flexShrink: 0,
    letterSpacing: '0.02em',
  },
  entryInfo: {
    display: 'flex',
    flexDirection: 'column',
    gap: 1,
    minWidth: 80,
    flexShrink: 0,
  },
  entryName: {
    fontSize: 13,
    fontWeight: 500,
    color: '#111',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    maxWidth: 90,
  },
  entryType: {
    fontSize: 10,
    color: 'var(--text-muted, #888)',
    textTransform: 'capitalize' as const,
  },
  checkboxGroup: {
    display: 'flex',
    flexWrap: 'wrap' as const,
    gap: 4,
    flex: 1,
  },
  checkLabel: {
    display: 'flex',
    alignItems: 'center',
    cursor: 'pointer',
    fontSize: 11,
    color: '#333',
    whiteSpace: 'nowrap' as const,
  },
  actionLabel: {
    fontSize: 11,
    color: '#444',
  },
  removeBtn: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    fontSize: 18,
    color: 'var(--text-muted, #888)',
    lineHeight: 1,
    padding: '0 4px',
    flexShrink: 0,
  },
  draftActions: {
    display: 'flex',
    gap: 8,
    alignItems: 'center',
    padding: '8px 0',
  },
  saveBtn: {
    padding: '6px 16px',
    background: '#111',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    transition: 'opacity 0.15s',
  },
  discardBtn: {
    padding: '6px 14px',
    border: '1px solid #ddd',
    borderRadius: 4,
    background: 'none',
    fontSize: 13,
    cursor: 'pointer',
    color: '#555',
  },
  divider: {
    borderTop: '1px solid var(--nav-border, #e8e8e8)',
    margin: '8px 0',
  },
  addRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    flexWrap: 'wrap' as const,
    paddingTop: 4,
  },
  select: {
    padding: '6px 10px',
    border: '1px solid #ddd',
    borderRadius: 4,
    fontSize: 13,
    color: '#111',
    background: '#fff',
    outline: 'none',
    minWidth: 160,
    maxWidth: 200,
  },
  addBtn: {
    padding: '6px 16px',
    background: '#111',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    flexShrink: 0,
    transition: 'opacity 0.15s',
  },
}
