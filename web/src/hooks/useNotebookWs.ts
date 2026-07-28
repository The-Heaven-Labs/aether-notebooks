import { useEffect, useRef, useCallback } from 'react'
import { getToken } from '../api/client'

export function useNotebookWs(
  notebookId: string | undefined,
  onCellOutput?: (cellId: string, outputs: Array<{ type: string; data: unknown }>, userEmail?: string, totalTimeMs?: number) => void,
  onCellMetadataChanged?: (cellId: string, metadata: Record<string, unknown>, userEmail?: string) => void,
  onCellUpdated?: (cellId: string, updates: Record<string, unknown>, userEmail?: string) => void,
  onCellCreated?: (cell: import('../types').Cell, userEmail?: string) => void,
  onCellDeleted?: (cellId: string, userEmail?: string) => void,
  onNotebookRefresh?: (reason?: string) => void,
  onCellExecuting?: (cellId: string) => void,
  onSync?: (data: { running_cells?: string[] }) => void,
) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
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

  const connect = useCallback(() => {
    if (!notebookId) return

    const token = getToken()
    if (!token) return

    const base = import.meta.env.VITE_API_URL || window.location.origin
    const wsBase = base.replace(/^http/, 'ws')
    const url = `${wsBase}/api/v1/ws/notebooks/${notebookId}?token=${token}`

    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        if (msg.type === 'cell_output' && onCellOutputRef.current) {
          onCellOutputRef.current(msg.cell_id, msg.outputs, msg.user_email, msg.total_time_ms)
        } else if (msg.type === 'cell_metadata_changed' && onCellMetadataChangedRef.current) {
          onCellMetadataChangedRef.current(msg.cell_id, msg.metadata, msg.user_email)
        } else if (msg.type === 'cell_updated' && onCellUpdatedRef.current) {
          const updates: Record<string, unknown> = {}
          for (const key of ['source', 'cell_type', 'language', 'source_visible', 'outputs_hidden', 'cell_collapsed', 'slide_break', 'title', 'description', 'slug', 'limit']) {
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
          onCellExecutingRef.current(msg.cell_id)
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
    }
  }, [connect])
}