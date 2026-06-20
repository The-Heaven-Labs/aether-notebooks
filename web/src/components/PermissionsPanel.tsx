import { useState, useEffect, useRef, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'

// ─── Types ───────────────────────────────────────────────────────────────────

type ResourceType = 'folder' | 'notebook' | 'connector' | 'dashboard' | 'agent' | 'model_config' | 'skill' | 'mcp_server' | 'tool'

const ACTION_LABELS: Record<ResourceType, string[]> = {
  folder:      ['view', 'create', 'edit', 'manage', 'delete'],
  notebook:    ['view', 'run', 'edit', 'share', 'delete'],
  connector:   ['view', 'use', 'edit', 'share', 'delete'],
  dashboard:   ['view', 'edit', 'share', 'delete', 'view_with_data'],
  agent:       ['view', 'edit', 'delete'],
  model_config:['view', 'edit', 'delete'],
  skill:       ['view', 'edit', 'delete'],
  mcp_server:  ['view', 'edit', 'delete'],
  tool:        ['view', 'use', 'edit', 'delete'],
}

const ACTION_DESCRIPTIONS: Record<ResourceType, Record<string, string>> = {
  connector: {
    view:   'See connector name, type, host, and status',
    use:    'Run SQL queries against the connector',
    edit:   'Edit connector configuration and credentials',
    share:  'Share this connector with others',
    delete: 'Delete the connector permanently',
  },
  notebook: {
    view:   'See notebook content and cell outputs',
    run:    'Execute notebook cells',
    edit:   'Edit cell contents and notebook settings',
    share:  'Share this notebook with others',
    delete: 'Delete the notebook permanently',
  },
  dashboard: {
    view:           'View the dashboard',
    edit:           'Edit dashboard layout and content',
    share:          'Share this dashboard with others',
    delete:         'Delete the dashboard permanently',
    view_with_data: 'View the dashboard including underlying cell data (bypass notebook permissions)',
  },
  folder: {
    view:   'See the folder and its contents',
    create: 'Create sub-folders and items',
    edit:   'Rename or restructure the folder',
    manage: 'Manage folder-level permissions',
    share:  'Share this folder with others',
    delete: 'Delete the folder permanently',
  },
  agent: {
    view:   'See agent details and configuration',
    edit:   'Edit agent settings and prompt',
    delete: 'Delete the agent permanently',
  },
  model_config: {
    view:   'See model configuration details',
    edit:   'Edit model configuration',
    delete: 'Delete the model configuration permanently',
  },
  skill: {
    view:   'See skill details',
    edit:   'Edit skill definition',
    delete: 'Delete the skill permanently',
  },
  mcp_server: {
    view:   'See MCP server details',
    edit:   'Edit MCP server configuration',
    delete: 'Delete the MCP server permanently',
  },
  tool: {
    view:   'See tool details',
    use:    'Use this tool in an agent',
    edit:   'Edit tool configuration',
    delete: 'Delete the tool permanently',
  },
}

interface AclEntry {
  id: string
  subject_type: 'user' | 'group' | 'org_role'
  subject_id: string
  actions: string[]
}

interface Member {
  user_id: string
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
  canEdit?: boolean
  resourceOwnerId?: string
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

function Avatar({ name, type }: { name: string; type: 'user' | 'group' | 'org_role' }) {
  return (
    <div style={styles.avatar}>
      {type === 'group' ? '#' : type === 'org_role' ? '★' : initials(name)}
    </div>
  )
}

// ─── Searchable Subject Selector ─────────────────────────────────────────────

interface SubjectSearchProps {
  members: Array<{ user_id: string; email: string; name?: string }>
  groups: Array<{ id: string; name: string }>
  resourceOwnerId?: string
  value: string
  onChange: (key: string) => void
}

function SubjectSearch({ members, groups, resourceOwnerId, value, onChange }: SubjectSearchProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [focusedIdx, setFocusedIdx] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLUListElement>(null)

  const availableMembers = members.filter(m => m.user_id !== resourceOwnerId)
  const displayLabel = value === 'org_role:everyone'
    ? 'Everyone (all members)'
    : value?.startsWith('user:')
      ? availableMembers.find(m => 'user:' + m.user_id === value)?.name
        || availableMembers.find(m => 'user:' + m.user_id === value)?.email
        || 'Selected'
      : value?.startsWith('group:')
        ? groups.find(g => 'group:' + g.id === value)?.name || 'Selected'
        : 'Select user, group, or Everyone…'

  const q = query.toLowerCase().trim()
  const filtered: Array<{ key: string; label: string; group: string }> = [
    { key: 'org_role:everyone', label: 'Everyone (all members)', group: '' },
    ...availableMembers
      .filter(m => !q || (m.name?.toLowerCase() || '').includes(q) || m.email.toLowerCase().includes(q))
      .map(m => ({ key: 'user:' + m.user_id, label: m.name || m.email, group: 'Users' })),
    ...groups
      .filter(g => !q || g.name.toLowerCase().includes(q))
      .map(g => ({ key: 'group:' + g.id, label: g.name, group: 'Groups' })),
  ]

  const selectOption = useCallback((key: string) => {
    onChange(key)
    setOpen(false)
    setQuery('')
    setFocusedIdx(-1)
  }, [onChange])

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
        setQuery('')
        setFocusedIdx(-1)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  useEffect(() => {
    if (open) inputRef.current?.focus()
  }, [open])

  useEffect(() => {
    if (focusedIdx >= 0 && listRef.current) {
      const el = listRef.current.children[focusedIdx] as HTMLElement
      el?.scrollIntoView?.({ block: 'nearest' })
    }
  }, [focusedIdx])

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setFocusedIdx(i => Math.min(i + 1, filtered.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setFocusedIdx(i => Math.max(i - 1, -1))
    } else if (e.key === 'Enter' && focusedIdx >= 0) {
      e.preventDefault()
      selectOption(filtered[focusedIdx].key)
    } else if (e.key === 'Escape') {
      setOpen(false)
      setQuery('')
      setFocusedIdx(-1)
    }
  }

  return (
    <div ref={containerRef} style={{ position: 'relative', flex: 1 }}>
      <button
        type="button"
        style={{
          width: '100%',
          textAlign: 'left',
          padding: '7px 10px',
          fontSize: 12,
          background: 'var(--bg-input)',
          color: value ? 'var(--text-primary)' : 'var(--text-muted)',
          border: '1px solid var(--border)',
          borderRadius: 4,
          cursor: 'pointer',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          gap: 8,
        }}
        onClick={() => { setOpen(v => !v); setFocusedIdx(-1) }}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{displayLabel}</span>
        <span style={{ flexShrink: 0, fontSize: 10, color: 'var(--text-muted)', transform: open ? 'rotate(180deg)' : undefined }}>▾</span>
      </button>
      {open && (
        <div style={{
          position: 'absolute',
          top: '100%',
          left: 0,
          right: 0,
          zIndex: 100,
          background: 'var(--bg-elevated)',
          border: '1px solid var(--border)',
          borderRadius: 4,
          boxShadow: '0 4px 16px rgba(0,0,0,0.15)',
          marginTop: 2,
        }}>
          <div style={{ padding: '6px 8px', borderBottom: '1px solid var(--border)' }}>
            <input
              ref={inputRef}
              style={{
                width: '100%',
                padding: '5px 8px',
                fontSize: 12,
                background: 'var(--bg-input)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border)',
                borderRadius: 3,
                outline: 'none',
                boxSizing: 'border-box',
              }}
              type="text"
              value={query}
              onChange={(e) => { setQuery(e.target.value); setFocusedIdx(-1) }}
              onKeyDown={handleKeyDown}
              placeholder="Search…"
              autoComplete="off"
            />
          </div>
          <ul ref={listRef} style={{
            listStyle: 'none',
            margin: 0,
            padding: '4px 0',
            maxHeight: 240,
            overflowY: 'auto',
          }} role="listbox">
            {filtered.length === 0 && (
              <li style={{ padding: '8px 12px', fontSize: 12, color: 'var(--text-muted)' }}>No matches found</li>
            )}
            {filtered.map((item, idx) => {
              const isEveryone = item.key === 'org_role:everyone'
              const isSelected = item.key === value
              const isFocused = idx === focusedIdx
              const showGroupHeader = idx === 0 && !q
                ? false
                : idx > 0 && filtered[idx - 1].group !== item.group
              return (
                <div key={item.key}>
                  {!q && showGroupHeader && item.group && (
                    <li style={{
                      padding: '3px 12px',
                      fontSize: 10,
                      fontWeight: 700,
                      color: 'var(--text-muted)',
                      textTransform: 'uppercase' as const,
                      letterSpacing: 0.5,
                      listStyle: 'none',
                    }}>{item.group}</li>
                  )}
                  <li
                    role="option"
                    aria-selected={isSelected}
                    style={{
                      padding: '6px 12px',
                      fontSize: 12,
                      cursor: 'pointer',
                      background: isFocused ? 'var(--accent-light)' : 'transparent',
                      color: isSelected ? 'var(--accent)' : isEveryone ? 'var(--text-primary)' : 'var(--text-primary)',
                      fontWeight: isEveryone ? 600 : 400,
                      listStyle: 'none',
                    }}
                    onMouseEnter={() => setFocusedIdx(idx)}
                    onMouseDown={(e) => { e.preventDefault(); selectOption(item.key) }}
                  >
                    {item.label}
                  </li>
                </div>
              )
            })}
          </ul>
        </div>
      )}
    </div>
  )
}

// ─── Component ───────────────────────────────────────────────────────────────

export function PermissionsPanel({
  resourceType,
  resourceId,
  resourceName,
  parentFolderId,
  canEdit = true,
  resourceOwnerId,
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
  const [expandedRows, setExpandedRows] = useState<Set<number>>(new Set())

  function setExpanded(idx: number, expanded: boolean) {
    setExpandedRows(prev => {
      const next = new Set(prev)
      if (expanded) next.add(idx)
      else next.delete(idx)
      return next
    })
  }

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

  const { data: parentFolder } = useQuery<{ id: string; name: string }>({
    queryKey: ['folder', parentFolderId],
    queryFn: () => api.get<{ id: string; name: string }>(`/api/v1/folders/${parentFolderId}`),
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
      const m = members.find((m) => m.user_id === entry.subject_id)
      return m ? (m.name || m.email) : entry.subject_id
    } else if (entry.subject_type === 'org_role') {
      return entry.subject_id === 'everyone' ? 'Everyone (all members)' : entry.subject_id
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
    setExpandedRows(prev => {
      const next = new Set(prev)
      next.add(entryIndex)
      return next
    })
  }

  function handleRemoveEntry(entryIndex: number) {
    const current = draft ?? aclData ?? []
    setDraft(current.filter((_, i) => i !== entryIndex))
  }

  function handleAddEntry() {
    if (!newSubjectKey || newActions.length === 0) return
    const [subjectType, subjectId] = newSubjectKey.split(':') as ['user' | 'group' | 'org_role', string]
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

  const allEntries = [
    ...(aclData ?? []).map((e: AclEntry) => ({ ...e, inherited: false })),
    ...(parentAcl ?? []).map((e: AclEntry) => ({ ...e, inherited: true })),
  ]

  const directEntries = allEntries.filter((e) => !e.inherited)
  const inheritedEntries = allEntries.filter((e) => e.inherited)
  const inheritedCount = inheritedEntries.length

  const visibleEntries = draft !== null ? draft : directEntries

  const typeBadgeColors: Record<string, string> = {
    folder: '#e8f0fe',
    notebook: '#fce8ff',
    connector: '#e8fff0',
    dashboard: '#fff8e8',
    agent: '#f0e8ff',
    model_config: '#e8fff0',
    skill: '#ffe8f0',
    mcp_server: '#fff0e8',
  }

  // ── Render ──

  return (
    <>
      {/* Backdrop */}
      <div style={styles.backdrop} onClick={onClose} />

      {/* Drawer */}
      <div style={styles.drawer} role="dialog" aria-labelledby="permissions-title">
        {/* Header */}
        <div style={styles.header}>
          <div style={styles.headerLeft}>
            <h2 id="permissions-title" style={styles.headerTitle}>
              {resourceName} <span style={styles.headerTitleSuffix}>permissions</span>
            </h2>
            <span style={{ ...styles.typeBadge, background: typeBadgeColors[resourceType] }} data-type={resourceType} className="permissions-panel-type-badge">
              {resourceType}
            </span>
          </div>
          <button style={styles.closeBtn} onClick={onClose} title="Close" aria-label="Close permissions dialog">×</button>
        </div>

        {/* Inheritance note */}
        <div style={styles.inheritNote}>
          {parentFolderId
            ? inheritedCount > 0
              ? `Inheriting ${inheritedCount} permission${inheritedCount === 1 ? '' : 's'} from parent folder`
              : 'Inheriting 0 permissions from parent folder'
            : 'No inherited permissions'}
        </div>

        {!canEdit && (
          <div style={styles.readOnlyNote}>
            You do not have permission to edit these permissions.
          </div>
        )}

        {/* Body */}
        <div style={styles.body}>
          {saveError && <div style={styles.errorText}>{saveError}</div>}

          {aclLoading ? (
            <div style={styles.loading}>Loading…</div>
          ) : (
            <>
              {/* Inherited permissions */}
              {inheritedEntries.length > 0 && (
                <div style={styles.inheritedSection}>
                  <div style={styles.inheritedHeader}>
                    <span style={styles.inheritedTitle}>
                      Inherited from {parentFolder?.name ?? 'parent folder'}
                    </span>
                    <span style={styles.readOnlyBadge}>read only</span>
                  </div>
                  {inheritedEntries.map((entry, idx) => (
                    <div key={`inherited-${entry.id || idx}`} style={{ ...styles.entryRow, opacity: 0.6 }}>
                      <Avatar name={subjectName(entry)} type={entry.subject_type} />
                      <div style={styles.entryInfo}>
                        <span style={styles.entryName}>{subjectName(entry)}</span>
                        <span style={styles.entryType}>{entry.subject_type}</span>
                      </div>
                      <div style={styles.checkboxGroup}>
                        {actions.map((action) => (
                          <label key={action} style={styles.checkLabel} title={ACTION_DESCRIPTIONS[resourceType]?.[action] ?? ''}>
                            <input
                              type="checkbox"
                              checked={entry.actions.includes(action)}
                              disabled
                              style={{ marginRight: 3 }}
                            />
                            <span style={styles.actionLabel}>{action.charAt(0).toUpperCase() + action.slice(1)}</span>
                          </label>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {/* Direct permissions */}
              {directEntries.length === 0 && !aclLoading && inheritedEntries.length === 0 && (
                <div style={styles.emptyText}>No permissions set. Add one below.</div>
              )}

              {directEntries.length === 0 && inheritedEntries.length > 0 && (
                <div style={styles.emptyText}>No direct permissions. Only inherited permissions above.</div>
              )}

              {visibleEntries.map((entry, idx) => (
                <div
                  key={entry.id || `direct-${idx}`}
                  tabIndex={0}
                  style={{ ...styles.entryRow, cursor: 'pointer' }}
                  onClick={() => setExpanded(idx, !expandedRows.has(idx))}
                  onBlur={(e) => {
                    if (!e.currentTarget.contains(e.relatedTarget as Node)) setExpanded(idx, false)
                  }}
                >
                  <div style={styles.entryRowHeader}>
                    <Avatar name={subjectName(entry)} type={entry.subject_type} />
                    <div style={styles.entryInfo}>
                      <span style={styles.entryName}>{subjectName(entry)}</span>
                      <span style={styles.entryType}>{entry.subject_type}</span>
                    </div>
                  </div>
                  {expandedRows.has(idx) && (
                    <div style={styles.expandedRow}>
                      <button
                        style={{ ...styles.removeBtn, opacity: canEdit ? 1 : 0.4 }}
                        title="Remove"
                        disabled={!canEdit}
                        onClick={(e) => {
                          e.stopPropagation()
                          if (canEdit) handleRemoveEntry(idx)
                        }}
                      >
                        ×
                      </button>

                      <div style={styles.checkboxGroup}>
                        {actions.map((action) => (
                          <label key={action} style={styles.checkLabel} title={ACTION_DESCRIPTIONS[resourceType]?.[action] ?? ''}>
                            <input
                              type="checkbox"
                              checked={entry.actions.includes(action)}
                              onChange={(e) => {
                                e.stopPropagation()
                                if (canEdit) handleToggleAction(idx, action)
                              }}
                              disabled={!canEdit}
                              style={{ marginRight: 3 }}
                            />
                            <span style={styles.actionLabel}>{action.charAt(0).toUpperCase() + action.slice(1)}</span>
                          </label>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              ))}

              {/* Save / Discard */}
              {draft !== null && canEdit && (
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
                <SubjectSearch
                  members={members}
                  groups={groups}
                  resourceOwnerId={resourceOwnerId}
                  value={newSubjectKey}
                  onChange={setNewSubjectKey}
                />

                <div style={styles.checkboxGroup}>
                  {actions.map((action) => (
                    <label key={action} style={styles.checkLabel} title={ACTION_DESCRIPTIONS[resourceType]?.[action] ?? ''}>
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
                    opacity: !canEdit || !newSubjectKey || newActions.length === 0 || saveAcl.isPending ? 0.5 : 1,
                  }}
                  disabled={!canEdit || !newSubjectKey || newActions.length === 0 || saveAcl.isPending}
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
    width: 480,
    background: 'var(--bg-card)',
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
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  headerLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    minWidth: 0,
  },
  headerTitle: {
    fontSize: 15,
    fontWeight: 700,
    color: 'var(--text-primary)',
    margin: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  headerTitleSuffix: {
    fontWeight: 400,
    color: 'var(--text-secondary)',
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
  readOnlyNote: {
    fontSize: 12,
    color: 'var(--text-muted)',
    padding: '8px 20px',
    background: 'var(--bg-secondary)',
    borderBottom: '1px solid var(--border)',
    fontStyle: 'italic',
  },
  body: {
    flex: 1,
    overflowX: 'hidden',
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
    padding: '10px 0',
    borderBottom: '1px solid var(--border)',
    outline: 'none',
  },
  entryRowHeader: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
  },
  expandedRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    marginTop: 8,
    paddingLeft: 42,
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
    minWidth: 120,
    maxWidth: 160,
    flexShrink: 0,
  },
  entryName: {
    fontSize: 13,
    fontWeight: 500,
    color: 'var(--text-primary)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    maxWidth: 150,
  },
  entryType: {
    fontSize: 10,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    textTransform: 'capitalize' as const,
  },
  checkboxGroup: {
    display: 'flex',
    flexWrap: 'wrap' as const,
    gap: 4,
    flex: 1,
    maxWidth: '100%',
    overflow: 'hidden',
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
    color: 'var(--text-primary)',
  },
  removeBtn: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    fontSize: 18,
    color: 'var(--text-muted)',
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
  inheritedSection: {
    marginBottom: 8,
    borderRadius: 6,
    border: '1px solid var(--border)',
    overflow: 'hidden',
  },
  inheritedHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px 12px',
    background: 'var(--bg-secondary)',
    borderBottom: '1px solid var(--border)',
  },
  inheritedTitle: {
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-secondary)',
  },
  readOnlyBadge: {
    fontSize: 10,
    fontWeight: 600,
    padding: '2px 6px',
    borderRadius: 3,
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    color: 'var(--text-muted)',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.04em',
  },
}
