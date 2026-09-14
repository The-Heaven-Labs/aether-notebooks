import { useEffect, useRef, useCallback } from 'react'
import { getToken } from '../api/client'
import { getWsUrl } from '../config'

// Scroll policy for run start: agent-initiated runs scroll into view (the
// viewer is not necessarily positioned at the cell), while human self-runs do
// not force a scroll (the runner is already looking at the cell). Locally
// initiated runs (pendingExec) never scroll.
export function shouldFlashExecutingCell(userEmail: string | undefined, pendingExec: Set<string>, cellId: string): boolean {
  return userEmail === 'agent@aether' && !pendingExec.has(cellId)
}

export function useNotebookWs(
  notebookId: string | undefined,
  onCellOutput?: (cellId: string, outputs: Array<{ type: string; data: unknown }>, userEmail?: string, totalTimeMs?: number) => void,
  onCellMetadataChanged?: (cellId: string, metadata: Record<string, unknown>, userEmail?: string) => void,
  onCellUpdated?: (cellId: string, updates: Record<string, unknown>, userEmail?: string) => void,
  onCellCreated?: (cell: import('../types').Cell, userEmail?: string) => void,
  onCellDeleted?: (cellId: string, userEmail?: string) => void,
  onNotebookRefresh?: (reason?: string) => void,
  onCellExecuting?: (cellId: string, startedAt?: string, userEmail?: string) => void,
  onSync?: (data: { running_cells?: Array<{ cell_id: string; started_at: string }> }) => void,
  onReconnect?: () => void,
) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const everConnectedRef = useRef(false)
  const onCellOutputRef = useRef(onCellOutput)
  onCellOutputRef.current = onCellOutput
  const onCellMetadataChangedRef = useRef(onCellMetadataChanged)
  onCellMetadataChangedRef.current = onCellMetadataChanged
  const onCellUpdatedRef = useRef(onCellUpdated)
  onCellUpdatedRef.current = onCellUpdated
  const onCellCreatedRef = useRef(onCellCreated)
  onCellCreatedRef.current = onCellCreated
  const onCellDeletedRef = useRef(onCellDeleted)
  onCellDeletedRef.current = onCellDeleted
  const onNotebookRefreshRef = useRef(onNotebookRefresh)
  onNotebookRefreshRef.current = onNotebookRefresh
  const onCellExecutingRef = useRef(onCellExecuting)
  onCellExecutingRef.current = onCellExecuting
  const onSyncRef = useRef(onSync)
  onSyncRef.current = onSync
  const onReconnectRef = useRef(onReconnect)
  onReconnectRef.current = onReconnect

  const connect = useCallback(() => {
    if (!notebookId) return

    const token = getToken()
    if (!token) return

    const wsBase = getWsUrl()
    const url = `${wsBase}/api/v1/ws/notebooks/${notebookId}?token=${token}`

    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      // A subsequent open means the socket dropped and reconnected — give
      // callers a chance to resync state that may have changed while offline.
      if (everConnectedRef.current) {
        onReconnectRef.current?.()
      }
      everConnectedRef.current = true
    }

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        if (msg.type === 'cell_output' && onCellOutputRef.current) {
          onCellOutputRef.current(msg.cell_id, msg.outputs, msg.user_email, msg.total_time_ms)
        } else if (msg.type === 'cell_metadata_changed' && onCellMetadataChangedRef.current) {
          onCellMetadataChangedRef.current(msg.cell_id, msg.metadata, msg.user_email)
        } else if (msg.type === 'cell_updated' && onCellUpdatedRef.current) {
          const updates: Record<string, unknown> = {}
          for (const key of ['source', 'cell_type', 'language', 'source_visible', 'outputs_hidden', 'cell_collapsed', 'slide_break', 'title', 'slug', 'limit', 'agent_updated_at', 'updated_at']) {
            if (msg[key] !== undefined) {
              updates[key === 'cell_type' ? 'type' : key] = msg[key]
            }
          }
          onCellUpdatedRef.current(msg.cell_id, updates, msg.user_email)
        } else if (msg.type === 'cell_created' && onCellCreatedRef.current) {
          onCellCreatedRef.current(msg.cell, msg.user_email)
        } else if (msg.type === 'cell_deleted' && onCellDeletedRef.current) {
          onCellDeletedRef.current(msg.cell_id, msg.user_email)
        } else if (msg.type === 'notebook_refresh' && onNotebookRefreshRef.current) {
          onNotebookRefreshRef.current(msg.reason)
        } else if (msg.type === 'cell_executing' && onCellExecutingRef.current) {
          onCellExecutingRef.current(msg.cell_id, msg.started_at, msg.user_email)
        } else if (msg.type === 'sync' && onSyncRef.current) {
          onSyncRef.current(msg)
        }
      } catch {
        // ignore non-JSON messages
      }
    }

    ws.onclose = () => {
      wsRef.current = null
      // Reconnect after 3 seconds if still mounted
      reconnectTimer.current = setTimeout(() => {
        if (!wsRef.current && notebookId) {
          connect()
        }
      }, 3000)
    }

    ws.onerror = () => {
      ws.close()
    }
  }, [notebookId])

  useEffect(() => {
    // Defer connection so React 18 Strict Mode cleanup runs before the
    // WebSocket is created — prevents "closed before established" error.
    const timer = setTimeout(() => connect(), 0)
    return () => {
      clearTimeout(timer)
      if (reconnectTimer.current) {
        clearTimeout(reconnectTimer.current)
        reconnectTimer.current = null
      }
      if (wsRef.current) {
        wsRef.current.onclose = null
        wsRef.current.close()
        wsRef.current = null
      }
      // A notebook switch is a fresh connection, not a reconnect.
      everConnectedRef.current = false
    }
  }, [connect])
}