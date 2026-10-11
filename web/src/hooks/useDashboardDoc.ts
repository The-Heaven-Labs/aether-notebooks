import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import type {
  DashboardAwareness,
  DashboardCollab,
  DashboardDocProjection,
} from '../components/dashboardCollabRuntime'
import type { Widget } from '../types'

/**
 * Live-dashboard document hook: acquires the ref-counted
 * `dashboard:{id}` Yjs document (see `dashboardCollabRuntime`), projects it
 * into plain React values, and exposes editor mutators.
 *
 * The collaboration runtime (Yjs + Hocuspocus) is imported on demand so it
 * stays out of the initial chunk of pages that only render the hook.
 *
 * Projection handover: pages should render from REST until `synced` is true
 * and then switch to the projected values (widget `created_at` stays empty —
 * merge REST timestamps if needed).
 */

type DashboardCollabModule = typeof import('../components/dashboardCollabRuntime')

/** Debounce for `setTitle` — title typing is harmless but high-frequency. */
export const DASHBOARD_TITLE_DEBOUNCE_MS = 400

export interface UseDashboardDocOptions {
  /**
   * Whether the caller may edit. Mutators are no-ops unless this is true
   * (default false), so read-only viewers can call the hook safely.
   * Connecting to the shared document is independent of this flag; pass an
   * empty `dashboardId` to skip it.
   */
  enabled?: boolean
}

export interface DashboardDocSnapshot {
  title: string
  settings: DashboardDocProjection['settings']
  variables: DashboardDocProjection['variables']
  widgets: Widget[]
  synced: boolean
  connected: boolean
}

export interface UseDashboardDocResult extends DashboardDocSnapshot {
  /** Provider awareness (presence) for `CollaboratorAvatars`; null until connected. */
  awareness: DashboardAwareness
  /** Moves/resizes a widget; each layout field merges independently. */
  updateLayout: (widgetId: string, layout: Widget['layout']) => void
  /** Replaces a widget's SQL text. */
  setQuery: (widgetId: string, query: string) => void
  /** Replaces a widget's chart config (stored as a JSON string). */
  setConfig: (widgetId: string, config: Record<string, unknown>) => void
  /** Sets the dashboard title; debounced by `DASHBOARD_TITLE_DEBOUNCE_MS`. */
  setTitle: (title: string) => void
}

const EMPTY_SNAPSHOT: DashboardDocSnapshot = {
  title: '',
  settings: {},
  variables: [],
  widgets: [],
  synced: false,
  connected: false,
}

interface ProjectionCache {
  doc: DashboardCollab['doc']
  projection: DashboardDocProjection
  synced: boolean
  connected: boolean
  snapshot: DashboardDocSnapshot
}

interface AcquiredRuntime {
  dashboardId: string
  mod: DashboardCollabModule
  entry: DashboardCollab
}

export function useDashboardDoc(
  dashboardId: string | null | undefined,
  options: UseDashboardDocOptions = {},
): UseDashboardDocResult {
  const editing = options.enabled ?? false
  const editingRef = useRef(editing)
  useEffect(() => { editingRef.current = editing }, [editing])

  const [runtime, setRuntime] = useState<AcquiredRuntime | null>(null)
  // The state entry carries its dashboard id so a stale entry from the
  // previous dashboard never projects while the next one loads.
  const active = runtime && runtime.dashboardId === dashboardId ? runtime : null
  const runtimeRef = useRef<AcquiredRuntime | null>(null)
  useEffect(() => { runtimeRef.current = active }, [active])

  const titleTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pendingTitleRef = useRef<string | null>(null)
  const projectionCacheRef = useRef<ProjectionCache | null>(null)

  // ── Acquire / release the shared document ──────────────────────────────────
  useEffect(() => {
    if (!dashboardId) return
    let cancelled = false
    let acquired: AcquiredRuntime | null = null

    void import('../components/dashboardCollabRuntime').then((mod) => {
      if (cancelled) return
      acquired = { dashboardId, mod, entry: mod.getDashboardCollab(dashboardId) }
      setRuntime(acquired)
    })

    return () => {
      cancelled = true
      projectionCacheRef.current = null
      if (!acquired) return
      // Flush a pending debounced title before dropping our reference so the
      // last keystroke is not lost (no-op when editing was never enabled).
      const pending = pendingTitleRef.current
      pendingTitleRef.current = null
      if (titleTimerRef.current !== null) {
        clearTimeout(titleTimerRef.current)
        titleTimerRef.current = null
      }
      if (pending !== null && editingRef.current) {
        acquired.mod.setDashboardTitle(acquired.entry, pending)
      }
      acquired.mod.releaseDashboardCollab(dashboardId)
    }
  }, [dashboardId])

  const entry = active?.entry ?? null
  const mod = active?.mod ?? null

  // ── External-store projection (doc updates + provider status) ──────────────
  const getSnapshot = useCallback((): DashboardDocSnapshot => {
    if (!entry || !mod) return EMPTY_SNAPSHOT
    const cached = projectionCacheRef.current
    const projection = cached && cached.doc === entry.doc
      ? cached.projection
      : mod.projectDashboardDoc(entry.doc, dashboardId ?? '')
    if (
      cached &&
      cached.doc === entry.doc &&
      cached.synced === entry.synced &&
      cached.connected === entry.connected
    ) {
      return cached.snapshot
    }
    const snapshot: DashboardDocSnapshot = {
      title: projection.title,
      settings: projection.settings,
      variables: projection.variables,
      widgets: projection.widgets,
      synced: entry.synced,
      connected: entry.connected,
    }
    projectionCacheRef.current = {
      doc: entry.doc,
      projection,
      synced: entry.synced,
      connected: entry.connected,
      snapshot,
    }
    return snapshot
  }, [entry, mod, dashboardId])

  const subscribe = useCallback((onStoreChange: () => void) => {
    if (!entry || !mod) return () => {}
    const { doc, provider } = entry

    // An update can land between render and subscription; recompute once here
    // and drop the cached projection only when it actually changed, so the
    // post-subscribe snapshot check catches it (and is a no-op otherwise).
    const cached = projectionCacheRef.current
    if (cached && cached.doc === doc) {
      const fresh = mod.projectDashboardDoc(doc, dashboardId ?? '')
      if (JSON.stringify(fresh) !== JSON.stringify(cached.projection)) {
        projectionCacheRef.current = null
      }
    }

    const onDocUpdate = () => {
      projectionCacheRef.current = null
      onStoreChange()
    }
    const onProviderState = () => onStoreChange()

    doc.on('update', onDocUpdate)
    provider.on('synced', onProviderState)
    provider.on('status', onProviderState)
    return () => {
      doc.off('update', onDocUpdate)
      provider.off('synced', onProviderState)
      provider.off('status', onProviderState)
    }
  }, [entry, mod, dashboardId])

  const snapshot = useSyncExternalStore(subscribe, getSnapshot, () => EMPTY_SNAPSHOT)

  // ── Editor mutators (no-ops unless `enabled`) ──────────────────────────────
  const updateLayout = useCallback((widgetId: string, layout: Widget['layout']) => {
    const rt = runtimeRef.current
    if (!editingRef.current || !rt) return
    rt.mod.updateWidgetLayout(rt.entry, widgetId, layout)
  }, [])

  const setQuery = useCallback((widgetId: string, query: string) => {
    const rt = runtimeRef.current
    if (!editingRef.current || !rt) return
    rt.mod.setWidgetQuery(rt.entry, widgetId, query)
  }, [])

  const setConfig = useCallback((widgetId: string, config: Record<string, unknown>) => {
    const rt = runtimeRef.current
    if (!editingRef.current || !rt) return
    rt.mod.setWidgetConfig(rt.entry, widgetId, config)
  }, [])

  const applyPendingTitle = useCallback(() => {
    if (titleTimerRef.current !== null) {
      clearTimeout(titleTimerRef.current)
      titleTimerRef.current = null
    }
    const pending = pendingTitleRef.current
    pendingTitleRef.current = null
    const rt = runtimeRef.current
    if (pending === null || !rt) return
    rt.mod.setDashboardTitle(rt.entry, pending)
  }, [])

  const setTitle = useCallback((title: string) => {
    if (!editingRef.current) return
    pendingTitleRef.current = title
    if (titleTimerRef.current !== null) clearTimeout(titleTimerRef.current)
    titleTimerRef.current = setTimeout(() => {
      titleTimerRef.current = null
      if (!editingRef.current) {
        pendingTitleRef.current = null
        return
      }
      applyPendingTitle()
    }, DASHBOARD_TITLE_DEBOUNCE_MS)
  }, [applyPendingTitle])

  return {
    title: snapshot.title,
    settings: snapshot.settings,
    variables: snapshot.variables,
    widgets: snapshot.widgets,
    synced: snapshot.synced,
    connected: snapshot.connected,
    awareness: entry?.provider.awareness ?? null,
    updateLayout,
    setQuery,
    setConfig,
    setTitle,
  }
}
