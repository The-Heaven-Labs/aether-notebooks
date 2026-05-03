import { useState, useEffect, useRef } from 'react'
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

const PRESETS: Record<ResourceType, Record<string, string[]>> = {
  folder:    { none: [], viewer: ['view'], editor: ['view', 'create', 'edit'], admin: ['view', 'create', 'edit', 'manage', 'delete'] },
  notebook:  { none: [], viewer: ['view'], editor: ['view', 'run', 'edit'], admin: ['view', 'run', 'edit', 'share', 'delete'] },
  connector: { none: [], viewer: ['view'], editor: ['view', 'use', 'edit'], admin: ['view', 'use', 'edit', 'share', 'delete'] },
  dashboard: { none: [], viewer: ['view'], editor: ['view', 'edit'], admin: ['view', 'edit', 'share', 'delete'] },
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
  const [selectOpen, setSelectOpen] = useState(false)
  const [selectSearch, setSelectSearch] = useState('')

  // Close custom select on outside click
  const selectRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!selectOpen) return
    const handler = (e: MouseEvent) => {
      if (selectRef.current && !selectRef.current.contains(e.target as Node)) {
        setSelectOpen(false)
        setSelectSearch('')
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [selectOpen])

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

  function applyPreset(entryIndex: number, preset: 'none' | 'viewer' | 'editor' | 'admin') {
    const current = draft ?? aclData ?? []
    const actions = PRESETS[resourceType][preset]
    const updated = current.map((e, i) =>
      i === entryIndex ? { ...e, actions } : e
    )
    setDraft(updated)
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
    folder: 'var(--accent-light)',
    notebook: 'var(--accent-light)',
    connector: 'var(--success-light)',
    dashboard: 'var(--warning-light)',
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
                  <div style={styles.presetRow}>
                    {(['none', 'viewer', 'editor', 'admin'] as const).map((preset) => (
                      <button
                        key={preset}
                        onClick={() => applyPreset(idx, preset)}
                        style={styles.presetBtn}
                      >
                        {preset}
                      </button>
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
                <div style={styles.customSelect} ref={selectRef}>
                  <button
                    type="button"
                    style={styles.customSelectTrigger}
                    onClick={() => setSelectOpen(!selectOpen)}
                  >
                    {newSubjectKey
                      ? (() => {
                          const [stype, sid] = newSubjectKey.split(':')
                          if (stype === 'user') {
                            const m = members.find(m => m.id === sid)
                            return m ? m.name || m.email : 'Unknown'
                          }
                          const g = groups.find(g => g.id === sid)
                          return g ? g.name : 'Unknown'
                        })()
                      : 'Select user or group…'}
                    <span style={styles.customSelectArrow}>▾</span>
                  </button>
                  {selectOpen && (
                    <div style={styles.customSelectDropdown}>
                      <div style={styles.customSelectSearch}>
                        <input
                          style={styles.customSelectSearchInput}
                          placeholder="Search…"
                          value={selectSearch}
                          onChange={(e) => setSelectSearch(e.target.value)}
                          autoFocus
                        />
                      </div>
                      {members.length > 0 && (
                        <div style={styles.customSelectGroup}>
                          <div style={styles.customSelectGroupLabel}>Users</div>
                          {members
                            .filter(m => !selectSearch || (m.name || m.email).toLowerCase().includes(selectSearch.toLowerCase()))
                            .map((m) => (
                              <button
                                key={m.id}
                                type="button"
                                style={{
                                  ...styles.customSelectItem,
                                  background: newSubjectKey === `user:${m.id}` ? 'var(--accent-light)' : 'transparent',
                                }}
                                onClick={() => { setNewSubjectKey(`user:${m.id}`); setSelectOpen(false); setSelectSearch('') }}
                              >
                                <span style={styles.customSelectItemName}>{m.name || m.email}</span>
                                <span style={styles.customSelectItemType}>user</span>
                              </button>
                            ))}
                        </div>
                      )}
                      {groups.length > 0 && (
                        <div style={styles.customSelectGroup}>
                          <div style={styles.customSelectGroupLabel}>Groups</div>
                          {groups
                            .filter(g => !selectSearch || g.name.toLowerCase().includes(selectSearch.toLowerCase()))
                            .map((g) => (
                              <button
                                key={g.id}
                                type="button"
                                style={{
                                  ...styles.customSelectItem,
                                  background: newSubjectKey === `group:${g.id}` ? 'var(--accent-light)' : 'transparent',
                                }}
                                onClick={() => { setNewSubjectKey(`group:${g.id}`); setSelectOpen(false); setSelectSearch('') }}
                              >
                                <span style={styles.customSelectItemName}>{g.name}</span>
                                <span style={styles.customSelectItemType}>group</span>
                              </button>
                            ))}
                        </div>
                      )}
                      {members.length === 0 && groups.length === 0 && (
                        <div style={styles.customSelectEmpty}>No users or groups available</div>
                      )}
                    </div>
                  )}
                </div>

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
    background: 'var(--bg-overlay)',
    zIndex: 1500,
  },
  drawer: {
    position: 'fixed',
    right: 0,
    top: 0,
    height: '100vh',
    width: 420,
    background: 'var(--bg-card)',
    boxShadow: 'var(--shadow-md)',
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
    borderBottom: '1px solid var(--border)',
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
    color: 'var(--text-primary)',
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
    color: 'var(--text-secondary)',
    flexShrink: 0,
  },
  closeBtn: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    fontSize: 22,
    color: 'var(--text-muted)',
    lineHeight: 1,
    padding: '0 4px',
    flexShrink: 0,
  },
  inheritNote: {
    fontSize: 12,
    color: 'var(--text-muted)',
    padding: '8px 20px',
    borderBottom: '1px solid var(--border)',
    background: 'var(--bg-secondary)',
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
    color: 'var(--text-muted)',
    padding: '8px 0',
  },
  emptyText: {
    fontSize: 13,
    color: 'var(--text-muted)',
    padding: '8px 0',
  },
  errorText: {
    fontSize: 12,
    color: 'var(--error-text)',
    background: 'var(--error-light)',
    border: '1px solid var(--error-border)',
    borderRadius: 4,
    padding: '8px 12px',
  },
  entryRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '10px 0',
    borderBottom: '1px solid var(--border)',
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
    color: 'var(--text-primary)',
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
  presetRow: {
    display: 'flex',
    gap: 4,
    marginTop: 4,
    flexShrink: 0,
  },
  presetBtn: {
    padding: '2px 8px',
    fontSize: 10,
    fontWeight: 600,
    borderRadius: 3,
    border: '1px solid var(--border)',
    background: 'transparent',
    color: 'var(--text-muted)',
    cursor: 'pointer',
    textTransform: 'capitalize' as const,
  },
  checkLabel: {
    display: 'flex',
    alignItems: 'center',
    cursor: 'pointer',
    fontSize: 11,
    color: 'var(--text-primary)',
    whiteSpace: 'nowrap' as const,
  },
  actionLabel: {
    fontSize: 11,
    color: 'var(--text-secondary)',
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
    background: 'var(--accent)',
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
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'none',
    fontSize: 13,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
  divider: {
    borderTop: '1px solid var(--border)',
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
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    color: 'var(--text-primary)',
    background: 'var(--bg-input)',
    outline: 'none',
    minWidth: 160,
    maxWidth: 200,
  },
  customSelect: {
    position: 'relative' as const,
    minWidth: 160,
    maxWidth: 200,
  },
  customSelectTrigger: {
    width: '100%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 6,
    padding: '6px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    color: 'var(--text-primary)',
    background: 'var(--bg-input)',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
    textAlign: 'left' as const,
  },
  customSelectArrow: {
    fontSize: 10,
    color: 'var(--text-muted)',
    flexShrink: 0,
  },
  customSelectDropdown: {
    position: 'absolute' as const,
    top: '100%',
    left: 0,
    right: 0,
    marginTop: 2,
    background: 'var(--bg-elevated)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    boxShadow: 'var(--shadow-md)',
    zIndex: 1600,
    maxHeight: 220,
    overflow: 'auto',
  },
  customSelectSearch: {
    padding: 6,
    borderBottom: '1px solid var(--border)',
  },
  customSelectSearchInput: {
    width: '100%',
    padding: '5px 8px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    color: 'var(--text-primary)',
    background: 'var(--bg-input)',
    outline: 'none',
    fontFamily: 'var(--font-sans)',
  },
  customSelectGroup: {
    borderBottom: '1px solid var(--border-light)',
  },
  customSelectGroupLabel: {
    padding: '4px 10px',
    fontSize: 10,
    fontWeight: 700,
    color: 'var(--text-muted)',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.06em',
    background: 'var(--bg-secondary)',
  },
  customSelectItem: {
    width: '100%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
    padding: '6px 10px',
    border: 'none',
    fontSize: 13,
    color: 'var(--text-primary)',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
    textAlign: 'left' as const,
    transition: 'background 0.1s',
  },
  customSelectItemName: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
  },
  customSelectItemType: {
    fontSize: 10,
    color: 'var(--text-muted)',
    fontWeight: 500,
    flexShrink: 0,
  },
  customSelectEmpty: {
    padding: '12px 10px',
    fontSize: 12,
    color: 'var(--text-muted)',
    textAlign: 'center' as const,
  },
  addBtn: {
    padding: '6px 16px',
    background: 'var(--accent)',
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
