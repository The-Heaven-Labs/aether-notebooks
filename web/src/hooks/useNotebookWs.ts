import { useEffect, useRef, useCallback } from 'react'
import { getToken } from '../api/client'

export function useNotebookWs(
  notebookId: string | undefined,
  onCellOutput?: (cellId: string, outputs: Array<{ type: string; data: unknown }>) => void,
  onCellMetadataChanged?: (cellId: string, metadata: Record<string, unknown>) => void,
  onCellUpdated?: (cellId: string, source?: string) => void,
) {
  const wsRef = useRef<WebSocket | null>(null)
  const onCellOutputRef = useRef(onCellOutput)
  onCellOutputRef.current = onCellOutput
  const onCellMetadataChangedRef = useRef(onCellMetadataChanged)
  onCellMetadataChangedRef.current = onCellMetadataChanged
  const onCellUpdatedRef = useRef(onCellUpdated)
  onCellUpdatedRef.current = onCellUpdated

  const connect = useCallback(() => {
    if (!notebookId) return

    const token = getToken()
    if (!token) return

    const base = import.meta.env.VITE_API_URL || ''
    const wsBase = base
      ? base.replace(/^http/, 'ws')
      : 'ws://localhost:8080'
    const url = `${wsBase}/api/v1/ws/notebooks/${notebookId}?token=${token}`

    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        if (msg.type === 'cell_output' && onCellOutputRef.current) {
          onCellOutputRef.current(msg.cell_id, msg.outputs)
        } else if (msg.type === 'cell_metadata_changed' && onCellMetadataChangedRef.current) {
          onCellMetadataChangedRef.current(msg.cell_id, msg.metadata)
        } else if (msg.type === 'cell_updated' && onCellUpdatedRef.current) {
          onCellUpdatedRef.current(msg.cell_id, msg.source)
        }
      } catch {
        // ignore non-JSON messages
      }
    }

    ws.onclose = () => {
      wsRef.current = null
      // Reconnect after 3 seconds if still mounted
      setTimeout(() => {
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
    connect()
    return () => {
      if (wsRef.current) {
        wsRef.current.onclose = null
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [connect])
}