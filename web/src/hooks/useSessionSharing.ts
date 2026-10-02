import { useCallback, useState } from 'react'
import { api } from '../api/client'
import type { AgentSession } from '../types/agent'

export interface SessionSharingState {
  notebookId: string | null
  inheritEnabled: boolean
  hasNotebook: boolean
}

interface SessionSharingResponse {
  notebook_id?: string | null
  share_with_notebook_viewers?: boolean
}

function stateFrom(res: SessionSharingResponse): SessionSharingState {
  const notebookId = res.notebook_id ?? null
  return {
    notebookId,
    inheritEnabled: res.share_with_notebook_viewers === true,
    hasNotebook: !!notebookId,
  }
}

/** Sharing state for one agent session: the notebook link and the
 * notebook-viewer inheritance flag. `open` fetches the authoritative session
 * before the dialog renders, and the PATCH responses keep the state in sync
 * (detaching a notebook clears inheritance server-side). */
export function useSessionSharing() {
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [state, setState] = useState<SessionSharingState | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const open = useCallback(async (id: string) => {
    setSessionId(id)
    setState(null)
    setError(null)
    setLoading(true)
    try {
      const sess = await api.get<AgentSession>(`/api/v1/sessions/${id}`)
      setState(stateFrom(sess))
    } catch {
      setError('Failed to load sharing settings')
    } finally {
      setLoading(false)
    }
  }, [])

  const close = useCallback(() => {
    setSessionId(null)
    setState(null)
    setError(null)
  }, [])

  const setNotebook = useCallback(async (notebookId: string | null) => {
    if (!sessionId) return
    const res = await api.patch<SessionSharingResponse>(
      `/api/v1/sessions/${sessionId}`,
      { notebook_id: notebookId },
    )
    setState(stateFrom(res))
  }, [sessionId])

  const setInherit = useCallback(async (enabled: boolean) => {
    if (!sessionId) return
    const res = await api.patch<SessionSharingResponse>(
      `/api/v1/sessions/${sessionId}`,
      { share_with_notebook_viewers: enabled },
    )
    setState(stateFrom(res))
  }, [sessionId])

  return { sessionId, state, loading, error, open, close, setNotebook, setInherit }
}
