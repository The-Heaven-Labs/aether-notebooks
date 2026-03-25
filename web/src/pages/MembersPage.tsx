import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Member } from '../types'
import { useAuth } from '../hooks/useAuth'

const ROLES = ['admin', 'editor', 'viewer'] as const

export function MembersPage() {
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
    <div style={styles.page}>
      <header style={styles.header}>
        <div style={styles.headerLeft}>
          <Link to="/" style={styles.backLink}>← Home</Link>
          <span style={styles.sep}>/</span>
          <span style={styles.pageTitle}>Members</span>
        </div>
      </header>

      <div style={styles.body}>
        {/* Invite form */}
        <div style={styles.formCard}>
          <h3 style={styles.formTitle}>Invite Member</h3>
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
              style={styles.inviteBtn}
              disabled={!inviteEmail.trim() || inviteMember.isPending}
              onClick={handleInvite}
            >
              {inviteMember.isPending ? 'Inviting…' : 'Invite'}
            </button>
          </div>
          {inviteError && <p style={styles.errorText}>{inviteError}</p>}
        </div>

        {/* Error banners for role/remove */}
        {roleError && <p style={styles.errorBanner}>{roleError}</p>}
        {removeError && <p style={styles.errorBanner}>{removeError}</p>}

        {/* Members table */}
        <div style={styles.tableWrap}>
          <table style={styles.table}>
            <thead>
              <tr>
                {['Name', 'Email', 'Role', 'Joined', ''].map((h) => (
                  <th key={h} style={styles.th}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
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
                  <tr key={m.user_id} style={styles.tr}>
                    <td style={styles.td}>
                      <strong>{m.name || '—'}</strong>
                      {isSelf && <span style={styles.selfBadge}>you</span>}
                    </td>
                    <td style={{ ...styles.td, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{m.email}</td>
                    <td style={styles.td}>
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
                    <td style={{ ...styles.td, color: 'var(--text-muted)', fontSize: 12 }}>{joinedDate}</td>
                    <td style={styles.tdActions}>
                      <button
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
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: '100vh',
    background: 'var(--bg-primary)',
    display: 'flex',
    flexDirection: 'column',
  },
  header: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    height: 52,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 32px',
    position: 'sticky',
    top: 0,
    zIndex: 100,
    flexShrink: 0,
  },
  headerLeft: { display: 'flex', alignItems: 'center', gap: 10 },
  backLink: { color: '#6a6260', textDecoration: 'none', fontSize: 13, fontWeight: 500 },
  sep: { color: '#3a3630', fontSize: 14 },
  pageTitle: { fontSize: 14, fontWeight: 600, color: 'var(--nav-text)' },
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  formCard: {
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 10,
    padding: 24,
    marginBottom: 24,
    boxShadow: 'var(--shadow-sm)',
  },
  formTitle: { margin: '0 0 14px', fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' },
  inviteRow: { display: 'flex', gap: 10, alignItems: 'center' },
  emailInput: {
    flex: 1,
    padding: '7px 12px',
    border: '1px solid var(--border)',
    borderRadius: 6,
    fontSize: 13,
    background: 'var(--bg-primary)',
    outline: 'none',
  },
  roleSelect: {
    padding: '7px 10px',
    border: '1px solid var(--border)',
    borderRadius: 6,
    fontSize: 13,
    background: 'white',
    cursor: 'pointer',
  },
  inviteBtn: {
    padding: '7px 18px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
  errorText: { color: 'var(--error)', fontSize: 12, margin: '10px 0 0' },
  errorBanner: {
    color: 'var(--error)',
    fontSize: 12,
    marginBottom: 12,
    padding: '8px 12px',
    background: '#fff0f0',
    border: '1px solid #fcd0d0',
    borderRadius: 6,
  },
  tableWrap: {
    borderRadius: 10,
    overflow: 'hidden',
    border: '1px solid var(--border)',
    boxShadow: 'var(--shadow-sm)',
  },
  table: { width: '100%', borderCollapse: 'collapse', background: 'white' },
  th: {
    padding: '10px 16px',
    textAlign: 'left',
    fontSize: 11,
    fontWeight: 700,
    color: 'var(--text-muted)',
    letterSpacing: '0.06em',
    borderBottom: '1px solid var(--border-light)',
    background: 'var(--bg-secondary)',
    textTransform: 'uppercase',
  },
  tr: { borderBottom: '1px solid var(--border-light)' },
  td: { padding: '12px 16px', fontSize: 13, color: 'var(--text-primary)' },
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
    background: 'var(--accent-light)',
    color: 'var(--accent)',
    padding: '1px 6px',
    borderRadius: 10,
    letterSpacing: '0.04em',
    textTransform: 'uppercase' as const,
  },
  roleSelectInline: {
    padding: '4px 8px',
    border: '1px solid var(--border)',
    borderRadius: 5,
    fontSize: 12,
    background: 'white',
    cursor: 'pointer',
  },
  roleSelectDisabled: {
    padding: '4px 8px',
    border: '1px solid var(--border-light)',
    borderRadius: 5,
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
    color: '#c0392b',
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
