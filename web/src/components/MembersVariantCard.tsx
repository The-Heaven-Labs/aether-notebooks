import type React from 'react'

export interface MemberCardItem {
  user_id: string
  name: string
  email: string
  role: 'admin' | 'editor' | 'viewer'
  joined_at: string
}

interface Props {
  members: MemberCardItem[]
  currentUserId?: string
  isAdmin?: boolean
  onRoleChange?: (userId: string, role: string) => void
  onRemove?: (userId: string) => void
}

const ROLES = ['admin', 'editor', 'viewer'] as const

const rolePalette: Record<string, { background: string; color: string }> = {
  admin:  { background: 'var(--error-light)', color: 'var(--error-text)' },
  editor: { background: 'var(--accent-light)', color: 'var(--text-primary)' },
  viewer: { background: 'var(--bg-secondary)', color: 'var(--text-secondary)' },
}

export function MembersVariantCard({ members, currentUserId, isAdmin, onRoleChange, onRemove }: Props) {
  if (members.length === 0) {
    return <div style={s.empty}>No members yet.</div>
  }

  return (
    <div style={s.list}>
      {members.map((m) => {
        const isSelf = m.user_id === currentUserId
        const displayName = m.name || m.email
        const initials = displayName.slice(0, 2).toUpperCase()
        const joined = new Date(m.joined_at).toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
        const palette = rolePalette[m.role] ?? rolePalette.viewer

        return (
          <div key={m.user_id} style={s.card}>
            <div style={s.row}>
              <div style={s.avatar}>{initials}</div>
              <div style={s.info}>
                <div style={s.nameRow}>
                  <span style={s.name}>{displayName}</span>
                  {isSelf && <span style={s.selfBadge}>you</span>}
                </div>
                <span style={s.email}>{m.email}</span>
              </div>
              <div style={s.right}>
                {isAdmin && !isSelf ? (
                  <select
                    style={s.roleSelect}
                    value={m.role}
                    onChange={(e) => onRoleChange?.(m.user_id, e.target.value)}
                    aria-label={`Role for ${displayName}`}
                  >
                    {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
                  </select>
                ) : (
                  <span style={{ ...s.roleBadge, ...palette }}>{m.role}</span>
                )}
                <span style={s.joined}>joined {joined}</span>
                {isAdmin && !isSelf && (
                  <button style={s.removeBtn} onClick={() => onRemove?.(m.user_id)} title={`Remove ${displayName}`}>×</button>
                )}
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}

const s: Record<string, React.CSSProperties> = {
  list: { display: 'flex', flexDirection: 'column', gap: 6 },
  empty: { padding: '40px 0', textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 },
  card: { border: '1px solid var(--border)', borderRadius: 6, overflow: 'hidden' },
  row: { display: 'flex', alignItems: 'center', gap: 12, padding: '10px 14px', background: 'var(--bg-card)' },
  avatar: {
    width: 32, height: 32, borderRadius: '50%',
    background: 'var(--text-primary)', color: 'var(--bg-card)',
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    fontSize: 11, fontWeight: 700, flexShrink: 0, letterSpacing: '0.02em',
  },
  info: { display: 'flex', flexDirection: 'column', gap: 1, flex: 1, minWidth: 0 },
  nameRow: { display: 'flex', alignItems: 'center', gap: 6 },
  name: { fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' },
  selfBadge: {
    fontSize: 10, fontWeight: 600, padding: '1px 6px', borderRadius: 3,
    background: 'var(--accent-light)', color: 'var(--text-primary)',
  },
  email: {
    fontSize: 12, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)',
    overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' as const,
  },
  right: { display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 },
  roleBadge: { fontSize: 11, fontWeight: 600, padding: '2px 8px', borderRadius: 3 },
  roleSelect: {
    fontSize: 11, fontWeight: 600, padding: '2px 6px', borderRadius: 3,
    border: '1px solid var(--border)', cursor: 'pointer', outline: 'none',
    background: 'var(--bg-input)', color: 'var(--text-primary)',
  },
  joined: { fontSize: 11, color: 'var(--text-muted)', whiteSpace: 'nowrap' as const },
  removeBtn: {
    padding: '2px 7px', fontSize: 14, fontWeight: 700,
    border: '1px solid transparent', borderRadius: 4,
    background: 'transparent', cursor: 'pointer', color: 'var(--error)', lineHeight: 1, flexShrink: 0,
  },
}
