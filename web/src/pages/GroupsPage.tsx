import { useState, useEffect, useRef, useCallback } from 'react'
import { AppShell } from '../components/AppShell'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Group, GroupMember, Member } from '../types'
import { useAuth } from '../hooks/useAuth'
import { ErrorBanner } from '../components/ErrorBanner'

// ─── MemberDropdown ──────────────────────────────────────────────────────────

interface MemberDropdownProps {
  options: Member[]
  value: string
  onChange: (userId: string) => void
  placeholder?: string
}

function MemberDropdown({ options, value, onChange, placeholder = 'Add member…' }: MemberDropdownProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [focusedIdx, setFocusedIdx] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLUListElement>(null)

  const selectedMember = options.find((m) => m.user_id === value)

  const filtered = query.trim()
    ? options.filter((m) => {
        const q = query.toLowerCase()
        return (m.name ?? '').toLowerCase().includes(q) || m.email.toLowerCase().includes(q)
      })
    : options

  const openDropdown = () => {
    setOpen(true)
    setQuery('')
    setFocusedIdx(-1)
    setTimeout(() => inputRef.current?.focus(), 0)
  }

  const closeDropdown = useCallback(() => {
    setOpen(false)
    setQuery('')
    setFocusedIdx(-1)
  }, [])

  const selectOption = (userId: string) => {
    onChange(userId)
    closeDropdown()
  }

  // Close on outside click
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        closeDropdown()
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open, closeDropdown])

  // Scroll focused item into view
  useEffect(() => {
    if (focusedIdx < 0 || !listRef.current) return
    const item = listRef.current.children[focusedIdx] as HTMLElement | undefined
    item?.scrollIntoView({ block: 'nearest' })
  }, [focusedIdx])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (!open) {
      if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
        e.preventDefault()
        openDropdown()
      }
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      closeDropdown()
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setFocusedIdx((i) => Math.min(i + 1, filtered.length - 1))
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      setFocusedIdx((i) => Math.max(i - 1, 0))
      return
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      if (focusedIdx >= 0 && filtered[focusedIdx]) {
        selectOption(filtered[focusedIdx].user_id)
      }
      return
    }
  }

  const displayLabel = selectedMember
    ? selectedMember.name ? `${selectedMember.name} (${selectedMember.email})` : selectedMember.email
    : placeholder

  return (
    <div ref={containerRef} style={dd.container}>
      {/* Trigger button */}
      <button
        type="button"
        style={{ ...dd.trigger, color: selectedMember ? 'var(--text-primary)' : 'var(--text-secondary)' }}
        onClick={() => open ? closeDropdown() : openDropdown()}
        onKeyDown={handleKeyDown}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span style={dd.triggerLabel}>{displayLabel}</span>
        <span style={{ ...dd.chevron, transform: open ? 'rotate(180deg)' : 'rotate(0deg)' }}>▾</span>
      </button>

      {/* Dropdown panel */}
      {open && (
        <div style={dd.panel}>
          {/* Search input */}
          <div style={dd.searchWrapper}>
            <input
              ref={inputRef}
              style={dd.searchInput}
              type="text"
              value={query}
              onChange={(e) => { setQuery(e.target.value); setFocusedIdx(-1) }}
              onKeyDown={handleKeyDown}
              placeholder="Search…"
              autoComplete="off"
            />
          </div>

          {/* Options list */}
          <ul ref={listRef} style={dd.list} role="listbox">
            {filtered.length === 0 && (
              <li style={dd.empty}>No members found</li>
            )}
            {filtered.map((m, idx) => {
              const isFocused = idx === focusedIdx
              const isSelected = m.user_id === value
              return (
                <li
                  key={m.user_id}
                  role="option"
                  aria-selected={isSelected}
                  style={{
                    ...dd.option,
                    background: isFocused ? 'var(--accent-light)' : isSelected ? 'var(--accent-light)' : 'transparent',
                    color: isSelected ? 'var(--accent)' : 'var(--text-primary)',
                  }}
                  onMouseEnter={() => setFocusedIdx(idx)}
                  onMouseDown={(e) => { e.preventDefault(); selectOption(m.user_id) }}
                >
                  <span style={dd.optionName}>{m.name || m.email}</span>
                  {m.name && <span style={dd.optionEmail}>{m.email}</span>}
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </div>
  )
}

const dd: Record<string, React.CSSProperties> = {
  container: {
    position: 'relative',
    flex: 1,
    minWidth: 0,
  },
  trigger: {
    width: '100%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 6,
    padding: '6px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-input)',
    cursor: 'pointer',
    textAlign: 'left' as const,
    fontFamily: 'var(--font-sans)',
  },
  triggerLabel: {
    flex: 1,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  chevron: {
    fontSize: 11,
    color: 'var(--text-secondary)',
    transition: 'transform 0.15s ease',
    flexShrink: 0,
  },
  panel: {
    position: 'absolute',
    top: 'calc(100% + 4px)',
    left: 0,
    right: 0,
    zIndex: 200,
    background: 'var(--bg-elevated)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    boxShadow: 'var(--shadow-md)',
    overflow: 'hidden',
  },
  searchWrapper: {
    padding: '6px 8px',
    borderBottom: '1px solid var(--border)',
  },
  searchInput: {
    width: '100%',
    padding: '5px 8px',
    fontSize: 12,
    border: '1px solid var(--border)',
    borderRadius: 3,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    outline: 'none',
    fontFamily: 'var(--font-sans)',
  },
  list: {
    listStyle: 'none',
    margin: 0,
    padding: '4px 0',
    maxHeight: 200,
    overflowY: 'auto',
  },
  option: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '6px 12px',
    fontSize: 13,
    cursor: 'pointer',
    transition: 'background 0.1s ease',
  },
  optionName: {
    fontWeight: 500,
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  optionEmail: {
    fontSize: 11,
    color: 'var(--text-secondary)',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    marginLeft: 'auto',
    flexShrink: 0,
  },
  empty: {
    padding: '8px 12px',
    fontSize: 12,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
}

export function GroupsPage() {
  useEffect(() => { document.title = "Groups — Heaven's Notebooks" }, [])
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const qc = useQueryClient()

  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [groupMembers, setGroupMembers] = useState<Record<string, GroupMember[]>>({})
  const [loadingMembers, setLoadingMembers] = useState<Record<string, boolean>>({})

  // Rename state: groupId → new name being edited
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')

  // New group form
  const [newGroupName, setNewGroupName] = useState('')
  const [createError, setCreateError] = useState<string | null>(null)
  const [mutateError, setMutateError] = useState<string | null>(null)

  // Add member selection per group
  const [selectedUserId, setSelectedUserId] = useState<Record<string, string>>({})

  const { data: groups = [], isLoading: groupsLoading } = useQuery({
    queryKey: ['groups'],
    queryFn: () => api.get<Group[]>('/api/v1/groups'),
  })

  const { data: members = [] } = useQuery({
    queryKey: ['members'],
    queryFn: () => api.get<Member[]>('/api/v1/members'),
    enabled: isAdmin,
  })

  const createGroup = useMutation({
    mutationFn: (name: string) => api.post<Group>('/api/v1/groups', { name }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['groups'] })
      setNewGroupName('')
      setCreateError(null)
    },
    onError: (err: Error) => setCreateError(err.message),
  })

  const updateGroup = useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) =>
      api.put<Group>(`/api/v1/groups/${id}`, { name }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['groups'] })
      setRenamingId(null)
      setMutateError(null)
    },
    onError: (err: Error) => setMutateError(err.message),
  })

  const deleteGroup = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/groups/${id}`),
    onSuccess: (_, id) => {
      qc.invalidateQueries({ queryKey: ['groups'] })
      if (expandedId === id) setExpandedId(null)
      setGroupMembers((prev) => {
        const next = { ...prev }
        delete next[id]
        return next
      })
      setMutateError(null)
    },
    onError: (err: Error) => setMutateError(err.message),
  })

  const addMember = useMutation({
    mutationFn: ({ groupId, userId }: { groupId: string; userId: string }) =>
      api.post(`/api/v1/groups/${groupId}/members`, { user_id: userId }),
    onSuccess: (_, { groupId, userId }) => {
      const cached = qc.getQueryData<Member[]>(['members']) ?? []
      const member = cached.find((m) => m.user_id === userId)
      if (member) {
        setGroupMembers((prev) => ({
          ...prev,
          [groupId]: [...(prev[groupId] ?? []), { user_id: member.user_id, name: member.name, email: member.email }],
        }))
      }
      qc.invalidateQueries({ queryKey: ['groups'] })
      setSelectedUserId((prev) => ({ ...prev, [groupId]: '' }))
      setMutateError(null)
    },
    onError: (err: Error) => setMutateError(err.message),
  })

  const removeMember = useMutation({
    mutationFn: ({ groupId, userId }: { groupId: string; userId: string }) =>
      api.delete(`/api/v1/groups/${groupId}/members/${userId}`),
    onSuccess: (_, { groupId, userId }) => {
      setGroupMembers((prev) => ({
        ...prev,
        [groupId]: (prev[groupId] ?? []).filter((m) => m.user_id !== userId),
      }))
      qc.invalidateQueries({ queryKey: ['groups'] })
      setMutateError(null)
    },
    onError: (err: Error) => setMutateError(err.message),
  })

  const handleToggleExpand = async (groupId: string) => {
    if (expandedId === groupId) {
      setExpandedId(null)
      return
    }
    setExpandedId(groupId)
    if (groupMembers[groupId] !== undefined) return

    setLoadingMembers((prev) => ({ ...prev, [groupId]: true }))
    try {
      const fetched = await api.get<GroupMember[]>(`/api/v1/groups/${groupId}/members`)
      setGroupMembers((prev) => ({ ...prev, [groupId]: fetched }))
    } catch (e) {
      setMutateError(e instanceof Error ? e.message : 'Failed to load members')
      setGroupMembers((prev) => ({ ...prev, [groupId]: [] }))
    } finally {
      setLoadingMembers((prev) => ({ ...prev, [groupId]: false }))
    }
  }

  const handleRename = (group: Group) => {
    setRenamingId(group.id)
    setRenameValue(group.name)
  }

  const handleRenameSubmit = (id: string) => {
    const trimmed = renameValue.trim()
    if (!trimmed) return
    updateGroup.mutate({ id, name: trimmed })
  }

  const handleDelete = (group: Group) => {
    if (!window.confirm(`Delete group "${group.name}"? This cannot be undone.`)) return
    deleteGroup.mutate(group.id)
  }

  const handleCreateGroup = () => {
    const trimmed = newGroupName.trim()
    if (!trimmed) {
      setCreateError('Group name is required')
      return
    }
    createGroup.mutate(trimmed)
  }

  const handleAddMemberClick = (groupId: string) => {
    const userId = selectedUserId[groupId]
    if (!userId) {
      setMutateError('Please select a member to add')
      return
    }
    addMember.mutate({ groupId, userId })
  }

  return (
    <AppShell>
      <div style={styles.body}>
        {/* Header */}
        <div style={styles.header}>
          <h1 style={styles.title}>Groups</h1>
          {isAdmin && (
            <div style={styles.createRow}>
              <input
                style={styles.input}
                type="text"
                value={newGroupName}
                onChange={(e) => setNewGroupName(e.target.value)}
                placeholder="New group name"
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && newGroupName.trim()) handleCreateGroup()
                }}
              />
              <button
                  type="button"
                  style={{
                    ...styles.primaryBtn,
                    opacity: (!newGroupName.trim() || createGroup.isPending) ? 0.5 : 1,
                    cursor: (!newGroupName.trim() || createGroup.isPending) ? 'not-allowed' : 'pointer',
                  }}
                  title={!newGroupName.trim() ? 'Enter a group name' : undefined}
                  disabled={!newGroupName.trim() || createGroup.isPending}
                  onClick={handleCreateGroup}
                >
                  {createGroup.isPending ? 'Creating…' : '+ New Group'}
                </button>
            </div>
          )}
          {createError && <ErrorBanner message={createError} onDismiss={() => setCreateError(null)} />}
          {mutateError && <ErrorBanner message={mutateError} onDismiss={() => setMutateError(null)} />}
        </div>

        {/* Group list */}
        <div style={styles.list}>
          {groupsLoading && (
            <div style={styles.empty}>Loading groups…</div>
          )}
          {!groupsLoading && groups.length === 0 && (
            <div style={styles.empty}>No groups yet. {isAdmin ? 'Create one above.' : ''}</div>
          )}
          {groups.map((group) => {
            const isExpanded = expandedId === group.id
            const currentMembers = groupMembers[group.id] ?? []
            const isLoadingGroup = loadingMembers[group.id] ?? false

            // Members not yet in the group
            const memberIdsInGroup = new Set(currentMembers.map((m) => m.user_id))
            const availableToAdd = members.filter((m) => !memberIdsInGroup.has(m.user_id))

            const isRenaming = renamingId === group.id

            return (
              <div key={group.id} style={styles.groupCard}>
                {/* Collapsed / header row */}
                <div style={{ ...styles.groupRow, borderRadius: isExpanded ? '6px 6px 0 0' : 6 }}>
                  <button
                    type="button"
                    style={styles.expandBtn}
                    onClick={() => handleToggleExpand(group.id)}
                    title={isExpanded ? 'Collapse' : 'Expand'}
                  >
                    <span style={{ ...styles.chevron, transform: isExpanded ? 'rotate(90deg)' : 'rotate(0deg)' }}>
                      ▶
                    </span>
                    {isRenaming ? (
                      <input
                        autoFocus
                        style={styles.renameInput}
                        value={renameValue}
                        onChange={(e) => setRenameValue(e.target.value)}
                        onClick={(e) => e.stopPropagation()}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') { e.stopPropagation(); handleRenameSubmit(group.id) }
                          if (e.key === 'Escape') { e.stopPropagation(); setRenamingId(null) }
                        }}
                      />
                    ) : (
                      <span style={styles.groupName}>{group.name}</span>
                    )}
                    <span style={styles.memberCount}>
                      {group.member_count} {group.member_count === 1 ? 'member' : 'members'}
                    </span>
                  </button>

                  {isAdmin && (
                    <div style={styles.actions}>
                      {isRenaming ? (
                        <>
                          <button
                            type="button"
                            style={styles.actionBtn}
                            onClick={() => handleRenameSubmit(group.id)}
                            disabled={updateGroup.isPending}
                          >
                            Save
                          </button>
                          <button
                            type="button"
                            style={styles.actionBtn}
                            onClick={() => setRenamingId(null)}
                          >
                            Cancel
                          </button>
                        </>
                      ) : (
                        <button
                          type="button"
                          style={styles.actionBtn}
                          onClick={() => handleRename(group)}
                        >
                          Rename
                        </button>
                      )}
                      <button
                        type="button"
                        style={styles.deleteBtn}
                        onClick={() => handleDelete(group)}
                        disabled={deleteGroup.isPending}
                      >
                        Delete
                      </button>
                    </div>
                  )}
                </div>

                {/* Expanded content */}
                {isExpanded && (
                  <div style={{ ...styles.expandedBody, borderRadius: '0 0 6px 6px' }}>
                    {isLoadingGroup && (
                      <div style={styles.loadingText}>Loading members…</div>
                    )}
                    {!isLoadingGroup && currentMembers.length === 0 && (
                      <div style={styles.emptyMembers}>No members in this group.</div>
                    )}
                    {!isLoadingGroup && currentMembers.map((m) => (
                      <div key={m.user_id} style={styles.memberRow}>
                        <span style={styles.memberName}>{m.name || m.email}</span>
                        <span style={styles.memberEmail}>{m.email}</span>
                        {isAdmin && (
<button
                          type="button"
                          style={styles.removeBtn}
                          title="Remove from group"
                          onClick={() => {
                            if (!window.confirm(`Remove ${m.name || m.email} from this group?`)) return
                            removeMember.mutate({ groupId: group.id, userId: m.user_id })
                          }}
                          disabled={removeMember.isPending}
                        >
                            ×
                          </button>
                        )}
                      </div>
                    ))}

                    {isAdmin && (
                      <div style={styles.addMemberRow}>
                        <MemberDropdown
                          options={availableToAdd}
                          value={selectedUserId[group.id] ?? ''}
                          onChange={(userId) =>
                            setSelectedUserId((prev) => ({ ...prev, [group.id]: userId }))
                          }
                        />
                        <button
                          type="button"
                          style={{
                            ...styles.primaryBtn,
                            opacity: (!selectedUserId[group.id] || addMember.isPending) ? 0.5 : 1,
                            cursor: (!selectedUserId[group.id] || addMember.isPending) ? 'not-allowed' : 'pointer',
                          }}
                          title={!selectedUserId[group.id] ? 'Select a member first' : undefined}
                          disabled={!selectedUserId[group.id] || addMember.isPending}
                          onClick={() => handleAddMemberClick(group.id)}
                        >
                          Add
                        </button>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 860, margin: '0 auto', padding: '32px 40px', width: '100%' },
  header: { marginBottom: 24 },
  title: { fontSize: 22, fontWeight: 700, margin: '0 0 16px 0' },
  createRow: { display: 'flex', gap: 10, alignItems: 'center', marginBottom: 8 },
  input: {
    flex: 1,
    padding: '7px 12px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    caretColor: 'var(--text-primary)',
    outline: 'none',
  },
  primaryBtn: {
    padding: '7px 16px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
  list: { display: 'flex', flexDirection: 'column', gap: 8 },
  empty: {
    padding: '40px 0',
    textAlign: 'center',
    color: 'var(--text-muted)',
    fontSize: 13,
  },
  groupCard: {
    border: '1px solid var(--border)',
    borderRadius: 6,
  },
  groupRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '10px 14px',
    background: 'var(--bg-primary)',
  },
  expandBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    background: 'transparent',
    border: 'none',
    cursor: 'pointer',
    padding: 0,
    flex: 1,
    textAlign: 'left' as const,
    minWidth: 0,
  },
  chevron: {
    fontSize: 10,
    color: 'var(--text-secondary)',
    transition: 'transform 0.15s ease',
    flexShrink: 0,
  },
  groupName: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  memberCount: {
    fontSize: 12,
    color: 'var(--text-secondary)',
    marginLeft: 8,
    fontWeight: 400,
  },
  renameInput: {
    fontSize: 14,
    fontWeight: 600,
    padding: '2px 6px',
    border: '1px solid var(--accent)',
    borderRadius: 3,
    outline: 'none',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    caretColor: 'var(--text-primary)',
    minWidth: 160,
  },
  actions: { display: 'flex', gap: 6, flexShrink: 0 },
  actionBtn: {
    padding: '4px 10px',
    fontSize: 12,
    fontWeight: 500,
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'transparent',
    cursor: 'pointer',
    color: 'inherit',
  },
  deleteBtn: {
    padding: '4px 10px',
    fontSize: 12,
    fontWeight: 500,
    border: '1px solid transparent',
    borderRadius: 4,
    background: 'transparent',
    cursor: 'pointer',
    color: 'var(--error)',
  },
  expandedBody: {
    borderTop: '1px solid var(--border)',
    padding: '12px 16px',
    background: 'var(--bg-secondary)',
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  loadingText: { fontSize: 13, color: 'var(--text-muted)', padding: '4px 0' },
  emptyMembers: { fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic', padding: '2px 0' },
  memberRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '4px 0',
  },
  memberName: {
    fontSize: 13,
    fontWeight: 500,
    minWidth: 140,
  },
  memberEmail: {
    fontSize: 12,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
    flex: 1,
  },
  removeBtn: {
    padding: '2px 7px',
    fontSize: 14,
    fontWeight: 700,
    border: '1px solid transparent',
    borderRadius: 4,
    background: 'transparent',
    cursor: 'pointer',
    color: 'var(--error)',
    lineHeight: 1,
    flexShrink: 0,
  },
  addMemberRow: {
    display: 'flex',
    gap: 8,
    alignItems: 'center',
    marginTop: 6,
    paddingTop: 8,
    borderTop: '1px solid var(--border)',
  },
}
