import type { Cell } from '../types'

export interface MergeCellContext {
  pendingExec: boolean
  dirty: boolean
}

// mergeServerCell reconciles a server-fetched cell with the locally cached
// copy. The server is authoritative except for source text with unsaved local
// edits (dirty) and outputs of a cell that is still executing locally.
export function mergeServerCell(local: Cell | undefined, server: Cell, ctx: MergeCellContext): Cell {
  if (!local) {
    return withDurationMetrics({ ...server })
  }
  if (local.source === server.source) {
    if (ctx.pendingExec) return local
    return { ...local, ...server }
  }
  return withDurationMetrics({
    ...server,
    source: ctx.dirty ? local.source : server.source,
    outputs: ctx.pendingExec ? local.outputs : server.outputs,
  })
}

// withDurationMetrics derives the per-phase metrics object the UI expects
// from a persisted duration_ms, leaving existing metrics untouched.
export function withDurationMetrics(cell: Cell): Cell {
  if (cell.duration_ms != null && !cell.metrics) {
    return {
      ...cell,
      metrics: { connect_time_ms: 0, query_time_ms: 0, render_time_ms: 0, total_time_ms: cell.duration_ms },
    }
  }
  return cell
}

// clearDirtyForSyncedCells removes cells from the dirty set whose local and
// server sources agree: an identical source means there is nothing unsaved, so
// a stale dirty flag must not keep resurrecting the local copy in merges.
export function clearDirtyForSyncedCells(dirty: Set<string>, locals: Cell[], servers: Cell[]): Set<string> {
  const next = new Set(dirty)
  for (const s of servers) {
    const l = locals.find(c => c.id === s.id)
    if (l && l.source === s.source) next.delete(s.id)
  }
  return next
}

// saveDelayFor returns the autosave debounce delay. While the agent updated the
// cell recently (< suppressWindowMs) the save is deferred until the suppression
// window closes instead of being dropped.
export function saveDelayFor(agentUpdatedAt: string | null | undefined, now: number, debounceMs = 1500, suppressWindowMs = 5000): number {
  if (!agentUpdatedAt) return debounceMs
  const elapsed = now - new Date(agentUpdatedAt).getTime()
  if (elapsed >= suppressWindowMs) return debounceMs
  return Math.max(debounceMs, suppressWindowMs - elapsed)
}
