import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { AppShell } from '../components/AppShell'
import { AgentPanel } from '../components/AgentPanel'
import { SessionViewer } from '../components/SessionViewer'
import { api, ApiError } from '../api/client'
import type { AgentSession } from '../types/agent'

type LoadError = 'forbidden' | 'notfound' | 'error'

// ChatPage is the /chats/:id route: the shareable deep link for an agent chat.
// Access is ACL-gated by the API — the link itself grants nothing. Editors
// (owner, or org admin in admin mode) get the interactive full-page panel;
// everyone else gets the read-only live SessionViewer.
export function ChatPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [session, setSession] = useState<AgentSession | null>(null)
  const [loadError, setLoadError] = useState<LoadError | null>(null)

  useEffect(() => {
    if (!id) return
    let cancelled = false
    setLoadError(null)
    api.get<AgentSession>(`/api/v1/sessions/${id}`)
      .then((s) => { if (!cancelled) setSession(s) })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 403) setLoadError('forbidden')
        else if (e instanceof ApiError && e.status === 404) setLoadError('notfound')
        else setLoadError('error')
      })
    return () => { cancelled = true }
  }, [id])

  useEffect(() => {
    document.title = session?.title ? `${session.title} · Aether` : 'Chat · Aether'
  }, [session?.title])

  // A deep link opened in a fresh tab has no history entry to return to;
  // fall back to Home so the Back affordance is never a dead control.
  const goBack = () => {
    const idx = (window.history.state as { idx?: number } | null)?.idx ?? 0
    if (idx > 0) navigate(-1)
    else navigate('/')
  }

  let body: ReactNode
  if (loadError) {
    const message = loadError === 'forbidden'
      ? "You don't have access to this chat. Ask the chat owner to share it with you."
      : loadError === 'notfound'
        ? 'Chat not found or has been deleted.'
        : 'Could not load this chat.'
    body = (
      <div style={styles.state}>
        <div style={styles.stateText}>{message}</div>
        <Link to="/" style={styles.stateLink}>Go home</Link>
      </div>
    )
  } else if (!session) {
    body = <div style={styles.state}><div style={styles.stateText}>Loading chat…</div></div>
  } else if (session.can_edit) {
    body = (
      <AgentPanel
        variant="page"
        initialSessionId={id}
        onSessionChange={(sid) => navigate(`/chats/${sid}`, { replace: true })}
        onClose={goBack}
      />
    )
  } else if (session.id === id) {
    body = <SessionViewer sessionId={id!} session={session} page onClose={goBack} />
  } else {
    body = <div style={styles.state}><div style={styles.stateText}>Loading chat…</div></div>
  }

  return (
    <AppShell noPadding>
      <div style={styles.page}>{body}</div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    display: 'flex',
    flexDirection: 'column',
    flex: 1,
    minHeight: 0,
  },
  state: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 12,
    flex: 1,
    padding: 40,
  },
  stateText: {
    color: 'var(--text-secondary)',
    fontSize: 14,
    textAlign: 'center',
    maxWidth: 420,
  },
  stateLink: {
    color: 'var(--accent)',
    fontSize: 13,
    textDecoration: 'none',
  },
}
