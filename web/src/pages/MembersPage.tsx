import { useState, useEffect } from 'react'
import { AppShell } from '../components/AppShell'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Member } from '../types'
import { useAuth } from '../hooks/useAuth'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { FormCard } from '../components/FormCard'
import { ErrorBanner } from '../components/ErrorBanner'
import { ConfirmDialog } from '../components/ConfirmDialog'

const ROLES = ['admin', 'non-admin'] as const

function formatRole(role: string): string {
  if (role === 'non-admin') return 'Non-Admin'
  return role.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())
}

export function MembersPage() {
  useEffect(() => { document.title = "Members — Aether Notebooks" }, [])
  const { user } = useAuth()
  const qc = useQueryClient()
  const isAdmin = user?.role === 'admin'

  useEffect(() => {
    api.get<{ invitations_enabled: boolean }>('/api/v1/org/invitations')
      .then(r => setInvitationsEnabled(r.invitations_enabled))
      .catch(() => {})
  }, [])

  const [showLinkForm, setShowLinkForm] = useState(false)
  const [linkRole, setLinkRole] = useState('viewer')
  const [generatedLink, setGeneratedLink] = useState<string | null>(null)
  const [linkError, setLinkError] = useState<string | null>(null)
  const [linkLoading, setLinkLoading] = useState(false)
  const [linkCopied, setLinkCopied] = useState(false)

  const [invitationsEnabled, setInvitationsEnabled] = useState(true)
  const [roleError, setRoleError] = useState<string | null>(null)
  const [removeError, setRemoveError] = useState<string | null>(null)
  const [removeTarget, setRemoveTarget] = useState<Member | null>(null)

  const { data: members = [], isLoading } = useQuery({
    queryKey: ['members'],
    queryFn: () => api.get<Member[]>('/api/v1/members'),
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

  const generateInviteLink = useMutation({
    mutationFn: () => api.post<{ token: string; url: string }>('/api/v1/members/invite-link', { role: linkRole }),
    onSuccess: (data) => {
      setGeneratedLink(data.url)
      setLinkError(null)
      setLinkLoading(false)
      // Auto-copy to clipboard
      navigator.clipboard.writeText(data.url).then(() => {
        setLinkCopied(true)
        setTimeout(() => setLinkCopied(false), 2000)
      }).catch(() => {})
    },
    onError: (err: Error) => {
      setLinkError(err.message)
      setLinkLoading(false)
    },
  })

  const handleRemove = (member: Member) => {
    setRemoveTarget(member)
  }

  return (
    <AppShell>
      <div style={styles.body}>
        <h1 style={{ fontSize: 16, fontWeight: 700, margin: '0 0 4px', color: 'var(--text-primary)' }}>Members</h1>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0, marginBottom: 24 }}>
          Manage organization members, roles, and invite links.
        </p>
        {/* Invite link generator — admin only when enabled */}
        {isAdmin && invitationsEnabled && (
          <div style={{ marginTop: 16 }}>
            <button
              type="button"
              style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 4, padding: '6px 12px', cursor: 'pointer', color: 'var(--text-secondary)', fontSize: 13 }}
              onClick={() => setShowLinkForm(!showLinkForm)}
            >
              {showLinkForm ? '− Hide' : '+ Generate invite link'}
            </button>
          </div>
        )}

        {isAdmin && invitationsEnabled && showLinkForm && (
          <FormCard title="Invite Link">
            <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 12 }}>
              <select
                style={styles.roleSelect}
                value={linkRole}
                onChange={(e) => setLinkRole(e.target.value)}
              >
                {ROLES.map((r) => (
                  <option key={r} value={r}>{formatRole(r)}</option>
                ))}
              </select>
              <button
                type="button"
                style={styles.inviteBtn}
                disabled={linkLoading}
                onClick={() => {
                  setLinkLoading(true)
                  setGeneratedLink(null)
                  generateInviteLink.mutate()
                }}
              >
                {linkLoading ? 'Generating…' : 'Generate'}
              </button>
            </div>
            {generatedLink && (
              <>
                <ErrorBanner
                  message="Link generated and copied to clipboard!"
                  variant="info"
                  onDismiss={() => {}}
                />
                <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 8 }}>
                  <input
                    style={{ ...styles.emailInput, flex: 1, fontFamily: 'var(--font-mono)', fontSize: 12, background: 'var(--accent-light)', border: '1px solid var(--accent)' }}
                    type="text"
                    value={generatedLink}
                    readOnly
                    onClick={(e) => (e.target as HTMLInputElement).select()}
                  />
                  <button
                    type="button"
                    style={{ ...styles.inviteBtn, padding: '7px 12px' }}
                    onClick={() => {
                      navigator.clipboard.writeText(generatedLink)
                      setLinkCopied(true)
                      setTimeout(() => setLinkCopied(false), 2000)
                    }}
                  >
                    {linkCopied ? '✓ Copied!' : 'Copy'}
                  </button>
                </div>
              </>
            )}
            {linkError && <ErrorBanner message={linkError} onDismiss={() => setLinkError(null)} />}
          </FormCard>
        )}

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
            const joinedDate = new Date(m.joined_at).toLocaleDateString('en-US', {
              month: 'short', day: 'numeric', year: 'numeric',
            })
            return (
              <tr key={m.user_id} style={rowStyle}>
                <td style={cellStyle}>
                  <strong>{m.name || '—'}</strong>
                  {isSelf && <span style={styles.selfBadge}>You</span>}
                </td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{m.email}</td>
                <td style={cellStyle}>
                  <select
                    style={isSelf || !isAdmin ? styles.roleSelectDisabled : styles.roleSelectInline}
                    value={m.role}
                    disabled={isSelf || !isAdmin}
                    onChange={(e) => updateRole.mutate({ userId: m.user_id, role: e.target.value })}
                  >
                    {ROLES.map((r) => (
                      <option key={r} value={r}>{formatRole(r)}</option>
                    ))}
                  </select>
                </td>
                <td style={{ ...cellStyle, color: 'var(--text-secondary)', fontSize: 12 }}>{joinedDate}</td>
                <td style={styles.tdActions}>
                  <button
                    type="button"
                    style={isSelf || !isAdmin ? styles.removeBtnDisabled : styles.removeBtn}
                    disabled={isSelf || !isAdmin || removeMember.isPending}
                    onClick={() => handleRemove(m)}
                    title={isSelf ? 'You cannot remove yourself from the organization' : !isAdmin ? 'Only admins can remove members' : undefined}
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
      <ConfirmDialog
        open={!!removeTarget}
        title="Remove member"
        message={`Remove ${removeTarget?.email} from the organization?`}
        confirmLabel="Remove"
        destructive
        onConfirm={() => { if (removeTarget) removeMember.mutate(removeTarget.user_id); setRemoveTarget(null) }}
        onCancel={() => setRemoveTarget(null)}
      />
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  inviteRow: { display: 'flex', gap: 10, alignItems: 'center' },
  emailInput: {
    flex: 1,
    padding: '7px 12px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    outline: 'none',
  },
  roleSelect: {
    padding: '7px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    cursor: 'pointer',
  },
  inviteBtn: {
    padding: '7px 18px',
    background: 'var(--accent)',
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
    color: 'var(--text-secondary)',
    fontSize: 13,
  },
  selfBadge: {
    marginLeft: 8,
    fontSize: 10,
    fontWeight: 700,
    background: 'var(--accent-light)',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    padding: '1px 6px',
    borderRadius: 3,
    letterSpacing: '0.02em',
  },
  roleSelectInline: {
    padding: '4px 8px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    cursor: 'pointer',
  },
  roleSelectDisabled: {
    padding: '4px 8px',
    border: '1px solid var(--border)',
    borderRadius: 4,
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
    color: 'var(--text-secondary)',
  },
}
