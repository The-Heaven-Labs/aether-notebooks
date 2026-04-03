import { useState, useEffect } from 'react'
import { AppShell } from '../components/AppShell'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Member } from '../types'
import { useAuth } from '../hooks/useAuth'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { FormCard } from '../components/FormCard'
import { ErrorBanner } from '../components/ErrorBanner'

const ROLES = ['admin', 'editor', 'viewer'] as const

export function MembersPage() {
  useEffect(() => { document.title = "Members — Heaven's Notebooks" }, [])
  const { user } = useAuth()
  const qc = useQueryClient()

  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState('viewer')
  const [inviteError, setInviteError] = useState<string | null>(null)

  const [roleError, setRoleError] = useState<string | null>(null)
  const [removeError, setRemoveError] = useState<string | null>(null)

  const { data: members = [], isLoading } = useQuery({
    queryKey: ['members'],
    queryFn: () => api.get<Member[]>('/api/v1/members'),
  })

  const inviteMember = useMutation({
    mutationFn: () => api.post<Member>('/api/v1/members', { email: inviteEmail.trim(), role: inviteRole }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['members'] })
      setInviteEmail('')
      setInviteRole('viewer')
      setInviteError(null)
    },
    onError: (err: Error) => setInviteError(err.message),
  })

  const updateRole = useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: string }) =>
      api.put(`/api/v1/members/${userId}`, { role }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['members'] })
      setRoleError(null)
    },
    onError: (err: Error) => setRoleError(err.message),
  })

  const removeMember = useMutation({
    mutationFn: (userId: string) => api.delete(`/api/v1/members/${userId}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['members'] })
      setRemoveError(null)
    },
    onError: (err: Error) => setRemoveError(err.message),
  })

  const handleInvite = () => {
    if (!inviteEmail.trim()) return
    inviteMember.mutate()
  }

  const handleRemove = (member: Member) => {
    if (!confirm(`Remove ${member.email} from the organization?`)) return
    removeMember.mutate(member.user_id)
  }

  return (
    <AppShell>
      <div style={styles.body}>
        {/* Invite form */}
        <FormCard title="Invite Member">
          <div style={styles.inviteRow}>
            <input
              style={styles.emailInput}
              type="email"
              value={inviteEmail}
              onChange={(e) => setInviteEmail(e.target.value)}
              placeholder="colleague@example.com"
              onKeyDown={(e) => { if (e.key === 'Enter' && inviteEmail.trim()) handleInvite() }}
            />
            <select
              style={styles.roleSelect}
              value={inviteRole}
              onChange={(e) => setInviteRole(e.target.value)}
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>{r.charAt(0).toUpperCase() + r.slice(1)}</option>
              ))}
            </select>
            <button
              type="button"
              style={styles.inviteBtn}
              disabled={!inviteEmail.trim() || inviteMember.isPending}
              onClick={handleInvite}
            >
              {inviteMember.isPending ? 'Inviting…' : 'Invite'}
            </button>
          </div>
          {inviteError && <ErrorBanner message={inviteError} onDismiss={() => setInviteError(null)} />}
        </FormCard>

        {/* Error banners for role/remove */}
        {roleError && <ErrorBanner message={roleError} onDismiss={() => setRoleError(null)} />}
        {removeError && <ErrorBanner message={removeError} onDismiss={() => setRemoveError(null)} />}

        {/* Members table */}
        <div style={{ marginTop: 24 }}>
          <StyledTable headers={['Name', 'Email', 'Role', 'Joined', 'Actions']}>
          {isLoading && (
            <tr>
              <td colSpan={5} style={styles.emptyCell}>Loading members…</td>
            </tr>
          )}
          {!isLoading && members.length === 0 && (
            <tr>
              <td colSpan={5} style={styles.emptyCell}>No members found.</td>
            </tr>
          )}
          {members.map((m) => {
            const isSelf = user?.user_id === m.user_id
            const joinedDate = new Date(m.joined_at).toLocaleDateString([], {
              month: 'short', day: 'numeric', year: 'numeric',
            })
            return (
              <tr key={m.user_id} style={rowStyle}>
                <td style={cellStyle}>
                  <strong>{m.name || '—'}</strong>
                  {isSelf && <span style={styles.selfBadge}>you</span>}
                </td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{m.email}</td>
                <td style={cellStyle}>
                  <select
                    style={isSelf ? styles.roleSelectDisabled : styles.roleSelectInline}
                    value={m.role}
                    disabled={isSelf}
                    onChange={(e) => updateRole.mutate({ userId: m.user_id, role: e.target.value })}
                  >
                    {ROLES.map((r) => (
                      <option key={r} value={r}>{r.charAt(0).toUpperCase() + r.slice(1)}</option>
                    ))}
                  </select>
                </td>
                <td style={{ ...cellStyle, color: 'var(--text-muted)', fontSize: 12 }}>{joinedDate}</td>
                <td style={styles.tdActions}>
                  <button
                    type="button"
                    style={isSelf ? styles.removeBtnDisabled : styles.removeBtn}
                    disabled={isSelf || removeMember.isPending}
                    onClick={() => handleRemove(m)}
                  >
                    Remove
                  </button>
                </td>
              </tr>
            )
          })}
          </StyledTable>
        </div>
      </div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  inviteRow: { display: 'flex', gap: 10, alignItems: 'center' },
  emailInput: {
    flex: 1,
    padding: '7px 12px',
    border: '1px solid #ddd',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-primary)',
    outline: 'none',
  },
  roleSelect: {
    padding: '7px 10px',
    border: '1px solid #ddd',
    borderRadius: 4,
    fontSize: 13,
    background: 'white',
    cursor: 'pointer',
  },
  inviteBtn: {
    padding: '7px 18px',
    background: '#111',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
  tdActions: { padding: '8px 16px', textAlign: 'right' as const },
  emptyCell: {
    padding: '40px 16px',
    textAlign: 'center' as const,
    color: 'var(--text-muted)',
    fontSize: 13,
  },
  selfBadge: {
    marginLeft: 8,
    fontSize: 10,
    fontWeight: 700,
    background: '#f5f5f5',
    color: '#666',
    border: '1px solid #e8e8e8',
    padding: '1px 6px',
    borderRadius: 3,
    letterSpacing: '0.04em',
    textTransform: 'uppercase' as const,
  },
  roleSelectInline: {
    padding: '4px 8px',
    border: '1px solid #ddd',
    borderRadius: 4,
    fontSize: 12,
    background: 'white',
    cursor: 'pointer',
  },
  roleSelectDisabled: {
    padding: '4px 8px',
    border: '1px solid var(--border-light)',
    borderRadius: 6,
    fontSize: 12,
    background: 'var(--bg-secondary)',
    color: 'var(--text-muted)',
    cursor: 'not-allowed',
  },
  removeBtn: {
    padding: '4px 10px',
    fontSize: 11,
    fontWeight: 600,
    border: '1px solid transparent',
    borderRadius: 4,
    background: 'transparent',
    cursor: 'pointer',
    color: 'var(--error-full)',
  },
  removeBtnDisabled: {
    padding: '4px 10px',
    fontSize: 11,
    fontWeight: 600,
    border: '1px solid transparent',
    borderRadius: 4,
    background: 'transparent',
    cursor: 'not-allowed',
    color: 'var(--text-muted)',
  },
}
