import { useState, useEffect } from 'react'
import { AppShell } from '../components/AppShell'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Group, GroupMember, Member } from '../types'
import { useAuth } from '../hooks/useAuth'
import { ErrorBanner } from '../components/ErrorBanner'

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
    if (!trimmed) return
    createGroup.mutate(trimmed)
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
                style={styles.primaryBtn}
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
                <div style={styles.groupRow}>
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
                  <div style={styles.expandedBody}>
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
                            onClick={() => removeMember.mutate({ groupId: group.id, userId: m.user_id })}
                            disabled={removeMember.isPending}
                          >
                            ×
                          </button>
                        )}
                      </div>
                    ))}

                    {isAdmin && (
                      <div style={styles.addMemberRow}>
                        <select
                          style={styles.memberSelect}
                          value={selectedUserId[group.id] ?? ''}
                          onChange={(e) =>
                            setSelectedUserId((prev) => ({ ...prev, [group.id]: e.target.value }))
                          }
                        >
                          <option value="">Add member…</option>
                          {availableToAdd.map((m) => (
                            <option key={m.user_id} value={m.user_id}>
                              {m.name ? `${m.name} (${m.email})` : m.email}
                            </option>
                          ))}
                        </select>
                        <button
                          type="button"
                          style={styles.primaryBtn}
                          disabled={!selectedUserId[group.id] || addMember.isPending}
                          onClick={() => {
                            const userId = selectedUserId[group.id]
                            if (userId) addMember.mutate({ groupId: group.id, userId })
                          }}
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
    border: '1px solid var(--nav-border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-primary)',
    outline: 'none',
  },
  primaryBtn: {
    padding: '7px 16px',
    background: '#111',
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
    border: '1px solid var(--nav-border)',
    borderRadius: 6,
    overflow: 'hidden',
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
    color: 'var(--text-muted)',
    transition: 'transform 0.15s ease',
    flexShrink: 0,
  },
  groupName: {
    fontSize: 14,
    fontWeight: 600,
    color: 'inherit',
  },
  memberCount: {
    fontSize: 12,
    color: 'var(--text-muted)',
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
    background: 'var(--bg-primary)',
    minWidth: 160,
  },
  actions: { display: 'flex', gap: 6, flexShrink: 0 },
  actionBtn: {
    padding: '4px 10px',
    fontSize: 12,
    fontWeight: 500,
    border: '1px solid var(--nav-border)',
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
    borderTop: '1px solid var(--nav-border)',
    padding: '12px 16px',
    background: 'var(--bg-secondary, #fafafa)',
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
    color: 'var(--text-muted)',
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
    borderTop: '1px solid var(--nav-border)',
  },
  memberSelect: {
    flex: 1,
    padding: '6px 10px',
    border: '1px solid var(--nav-border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-primary)',
    cursor: 'pointer',
  },
}
