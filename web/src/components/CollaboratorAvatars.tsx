import { useEffect, useState, useRef } from 'react'
import { HocuspocusProvider } from '@hocuspocus/provider'

interface Collaborator {
  email: string
  name: string
  color: string
  focus: { cellId: string | null; scrollTop: number | null } | null
  following: { email: string | null } | null
}

interface CollaboratorAvatarsProps {
  provider: HocuspocusProvider | undefined
  currentUserEmail: string
  following: { email: string; name: string } | null
  onFollow: (collab: { email: string; name: string }) => void
  onUnfollow: () => void
  showAgent: boolean
  onFollowAgent: () => void
}

const MAX_VISIBLE = 4
const AGENT_COLOR = '#8b5cf6'
const AGENT_EMAIL = 'agent@hnb'

function initials(name: string): string {
  const parts = name.trim().split(/\s+/)
  if (parts.length >= 2) return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
  return (name[0] ?? '?').toUpperCase()
}

export function CollaboratorAvatars({
  provider,
  currentUserEmail,
  following,
  onFollow,
  onUnfollow,
  showAgent,
  onFollowAgent,
}: CollaboratorAvatarsProps) {
  const [collaborators, setCollaborators] = useState<Collaborator[]>([])
  const [followedByMe, setFollowedByMe] = useState(false)
  const [followersOfMe, setFollowersOfMe] = useState<string[]>([])
  const [showOverflow, setShowOverflow] = useState(false)
  const overflowRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const awareness = provider?.awareness
    if (!awareness) {
      setCollaborators([])
      return
    }

    const handler = () => {
      const states = awareness.getStates()
      const list: Collaborator[] = []
      const followers: string[] = []

      for (const [, state] of states as any) {
        const user = state.user
        if (!user || user.email === currentUserEmail) continue

        list.push({
          email: user.email,
          name: user.name,
          color: user.color,
          focus: state.focus ?? null,
          following: state.following ?? null,
        })

        if (state.following?.email === currentUserEmail) {
          followers.push(user.email)
        }
      }

      list.sort((a, b) => {
        if (following && a.email === following.email) return -1
        if (following && b.email === following.email) return 1
        return a.name.localeCompare(b.name)
      })

      setCollaborators(list)
      setFollowersOfMe(followers)
      setFollowedByMe(following?.email === 'agent@hnb')
    }

    awareness.on('change', handler)
    handler()

    return () => awareness.off('change', handler)
  }, [provider, currentUserEmail, following])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && following) {
        onUnfollow()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [following, onUnfollow])

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (overflowRef.current && !overflowRef.current.contains(e.target as Node)) {
        setShowOverflow(false)
      }
    }
    if (showOverflow) {
      document.addEventListener('mousedown', handler)
      return () => document.removeEventListener('mousedown', handler)
    }
  }, [showOverflow])

  const visible = collaborators.slice(0, MAX_VISIBLE)
  const overflow = collaborators.slice(MAX_VISIBLE)
  const overflowCount = overflow.length + (showAgent && !visible.some(c => c.email === AGENT_EMAIL) ? 1 : 0)

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 4, position: 'relative' }}>
      {visible.map((c) => (
        <div key={c.email} style={{ position: 'relative' }}>
          <button
            type="button"
            onClick={() => {
              if (following?.email === c.email) {
                onUnfollow()
              } else {
                onFollow({ email: c.email, name: c.name })
              }
            }}
            title={c.name}
            style={{
              width: 28,
              height: 28,
              borderRadius: '50%',
              background: c.color,
              color: '#fff',
              border: following?.email === c.email ? '2px solid var(--accent)' : '2px solid transparent',
              fontSize: 11,
              fontWeight: 600,
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              padding: 0,
              lineHeight: 1,
              transition: 'border-color 0.15s',
              position: 'relative',
            }}
          >
            {initials(c.name)}
          </button>
          {followersOfMe.includes(c.email) && (
            <div
              style={{
                position: 'absolute',
                bottom: 0,
                right: 0,
                width: 6,
                height: 6,
                borderRadius: '50%',
                background: 'var(--accent)',
                border: '1px solid var(--bg-card)',
              }}
            />
          )}
        </div>
      ))}

      {showAgent && (
        <div style={{ position: 'relative' }}>
          <button
            type="button"
            onClick={onFollowAgent}
            title="AI Agent"
            style={{
              width: 28,
              height: 28,
              borderRadius: '50%',
              background: AGENT_COLOR,
              color: '#fff',
              border: followedByMe ? '2px solid var(--accent)' : '2px solid transparent',
              fontSize: 11,
              fontWeight: 600,
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              padding: 0,
              lineHeight: 1,
              transition: 'border-color 0.15s',
            }}
          >
            AI
          </button>
        </div>
      )}

      {overflowCount > 0 && (
        <div ref={overflowRef} style={{ position: 'relative' }}>
          <button
            type="button"
            onClick={() => setShowOverflow(!showOverflow)}
            style={{
              width: 28,
              height: 28,
              borderRadius: '50%',
              background: 'var(--bg-secondary)',
              color: 'var(--text-muted)',
              border: '1px solid var(--border)',
              fontSize: 11,
              fontWeight: 600,
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              padding: 0,
              lineHeight: 1,
            }}
          >
            +{overflowCount}
          </button>
          {showOverflow && (
            <div
              style={{
                position: 'absolute',
                top: '100%',
                right: 0,
                marginTop: 4,
                background: 'var(--bg-card)',
                border: '1px solid var(--border)',
                borderRadius: 6,
                boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
                zIndex: 100,
                minWidth: 160,
                padding: '4px 0',
              }}
            >
              {overflow.map((c) => (
                <button
                  key={c.email}
                  type="button"
                  onClick={() => {
                    if (following?.email === c.email) {
                      onUnfollow()
                    } else {
                      onFollow({ email: c.email, name: c.name })
                    }
                    setShowOverflow(false)
                  }}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    width: '100%',
                    padding: '6px 12px',
                    border: 'none',
                    background: 'none',
                    cursor: 'pointer',
                    fontSize: 13,
                    color: 'var(--text-primary)',
                    textAlign: 'left',
                  }}
                >
                  <div
                    style={{
                      width: 20,
                      height: 20,
                      borderRadius: '50%',
                      background: c.color,
                      color: '#fff',
                      fontSize: 9,
                      fontWeight: 600,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      flexShrink: 0,
                    }}
                  >
                    {initials(c.name)}
                  </div>
                  <span style={{ flex: 1 }}>{c.name}</span>
                  {following?.email === c.email && (
                    <span style={{ color: 'var(--accent)', fontSize: 11 }}>Following</span>
                  )}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
