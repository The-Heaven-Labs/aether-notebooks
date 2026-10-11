import * as Y from 'yjs'
import { HocuspocusProvider } from '@hocuspocus/provider'
import { getRelayUrl } from '../config'
import type { Dashboard, DashboardVariable, Widget } from '../types'

/**
 * Collaboration runtime for live dashboards: a ref-counted Yjs document +
 * Hocuspocus relay provider per dashboard, plus the pure projections and
 * mutators `useDashboardDoc` runs against them.
 *
 * Imported lazily (from the hook) so the Yjs + Hocuspocus stack stays out of
 * the dashboard pages' initial chunks.
 *
 * Document shape (source of truth: internal/dashboarddoc/doc.go):
 *
 *   meta     Y.Map { title: string, settings: Y.Map, variables: Y.Array<Y.Map> }
 *   widgets  Y.Map<widgetId, Y.Map { type, connector_id, language, notebook_id,
 *            cell_id (strings, "" = unset), config: JSON string,
 *            layout: Y.Map { row, col, width, height }, query: Y.Text }>
 *
 * Relay document name: `dashboard:{id}`.
 */

export interface DashboardCollab {
  doc: Y.Doc
  provider: HocuspocusProvider
  refCount: number
  synced: boolean
  connected: boolean
}

/** Awareness instance of a dashboard's provider (null before/after connect). */
export type DashboardAwareness = DashboardCollab['provider']['awareness']

/** Shared runtime registry: one entry per dashboard with a live consumer. */
export const dashboardCollabCache = new Map<string, DashboardCollab>()

function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  return h
}

/** Creates and registers the shared document + relay provider for a dashboard. */
export function createDashboardCollab(dashboardId: string, cache: Map<string, DashboardCollab>): DashboardCollab {
  const doc = new Y.Doc()
  const token = localStorage.getItem('aether_token') ?? ''
  const userName = localStorage.getItem('aether_user_name') ?? ''
  const userEmail = localStorage.getItem('aether_user_email') ?? ''

  const provider = new HocuspocusProvider({
    url: getRelayUrl(),
    name: `dashboard:${dashboardId}`,
    document: doc,
    token,
    onAuthenticationFailed: () => console.warn('[yjs] Dashboard relay auth failed'),
  })

  provider.awareness?.setLocalStateField('user', {
    name: userName || userEmail || 'Anonymous',
    email: userEmail,
    color: `hsl(${Math.abs(hashStr(userEmail || userName)) % 360}, 70%, 55%)`,
  })

  const entry: DashboardCollab = { doc, provider, refCount: 1, synced: false, connected: false }
  provider.on('synced', ({ state }: { state: boolean }) => { if (state) entry.synced = true })
  provider.on('status', ({ status }: { status: string }) => { entry.connected = status === 'connected' })
  cache.set(dashboardId, entry)
  return entry
}

/** Non-creating lookup: returns the shared entry only if one exists. */
export function peekDashboardCollab(dashboardId: string): DashboardCollab | undefined {
  return dashboardCollabCache.get(dashboardId)
}

/**
 * Returns the shared collab entry for a dashboard, creating the Yjs document
 * and relay provider on first use. Synchronous and race-free: the dynamic
 * import is deduplicated by the module loader, so concurrent callers run the
 * check-then-create sequence sequentially.
 */
export function getDashboardCollab(dashboardId: string): DashboardCollab {
  const existing = dashboardCollabCache.get(dashboardId)
  if (existing) {
    existing.refCount++
    return existing
  }
  return createDashboardCollab(dashboardId, dashboardCollabCache)
}

/** Drops one reference; destroys the provider + doc on the last release. */
export function releaseDashboardCollab(dashboardId: string): void {
  const entry = dashboardCollabCache.get(dashboardId)
  if (!entry) return
  entry.refCount--
  if (entry.refCount <= 0) {
    try { entry.provider.destroy() } catch { /* ignore */ }
    entry.doc.destroy()
    dashboardCollabCache.delete(dashboardId)
  }
}

/** Awareness access for components that render presence without the hook. */
export function getDashboardAwareness(dashboardId: string): DashboardAwareness {
  return dashboardCollabCache.get(dashboardId)?.provider.awareness ?? null
}

// ── Projection ────────────────────────────────────────────────────────────────

/** Doc-sourced dashboard settings; variables are projected separately. */
export type DashboardDocSettings = Omit<Dashboard['settings'], 'variables'>

export interface DashboardDocProjection {
  title: string
  settings: DashboardDocSettings
  variables: DashboardVariable[]
  widgets: Widget[]
}

const WIDGET_TYPES = new Set<Widget['type']>(['chart', 'table', 'metric', 'text'])

/**
 * Converts the shared document into plain values for React.
 *
 * `created_at` is not part of the document, so projected widgets carry an
 * empty timestamp; pages that need real timestamps merge them from the REST
 * widget rows (the doc is the source of truth only for co-edited fields).
 * `dashboard_id` comes from the caller because the document does not store it.
 * Unset string fields ("") and an empty query project as null, matching the
 * REST widget rows.
 */
export function projectDashboardDoc(doc: Y.Doc, dashboardId: string): DashboardDocProjection {
  const meta = doc.getMap('meta')
  const rawTitle = meta.get('title')
  const title = typeof rawTitle === 'string' ? rawTitle : ''

  const settings: Record<string, unknown> = {}
  const settingsMap = meta.get('settings')
  if (settingsMap instanceof Y.Map) {
    for (const [key, value] of settingsMap.entries()) {
      settings[key] = toPlainValue(value)
    }
  }

  const variables: DashboardVariable[] = []
  const variablesArray = meta.get('variables')
  if (variablesArray instanceof Y.Array) {
    for (const item of variablesArray.toArray()) {
      if (item instanceof Y.Map) variables.push(item.toJSON() as DashboardVariable)
    }
  }

  const widgets: Widget[] = []
  const widgetsMap = doc.getMap('widgets')
  const ids = Array.from(widgetsMap.keys()).sort()
  for (const id of ids) {
    const wm = widgetsMap.get(id)
    if (!(wm instanceof Y.Map)) continue
    const type = wm.get('type')
    if (typeof type !== 'string' || !WIDGET_TYPES.has(type as Widget['type'])) continue
    const queryText = wm.get('query')
    const query = queryText instanceof Y.Text ? queryText.toString() : ''
    widgets.push({
      id,
      dashboard_id: dashboardId,
      type: type as Widget['type'],
      layout: projectLayout(wm.get('layout')),
      config: projectConfig(wm.get('config')),
      query: query === '' ? null : query,
      language: typeof wm.get('language') === 'string' ? (wm.get('language') as string) : '',
      connector_id: emptyToNull(wm.get('connector_id')),
      notebook_id: emptyToNull(wm.get('notebook_id')),
      cell_id: emptyToNull(wm.get('cell_id')),
      created_at: '',
    })
  }

  return { title, settings: settings as DashboardDocSettings, variables, widgets }
}

function projectLayout(value: unknown): Widget['layout'] {
  const lm = value instanceof Y.Map ? value : null
  return {
    row: layoutNumber(lm?.get('row')),
    col: layoutNumber(lm?.get('col')),
    width: layoutNumber(lm?.get('width')),
    height: layoutNumber(lm?.get('height')),
  }
}

function layoutNumber(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function projectConfig(value: unknown): Record<string, unknown> {
  if (typeof value !== 'string' || value.trim() === '') return {}
  try {
    const parsed: unknown = JSON.parse(value)
    if (parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
  } catch { /* malformed config falls back to an empty object */ }
  return {}
}

function emptyToNull(value: unknown): string | null {
  return typeof value === 'string' && value !== '' ? value : null
}

/** Unwraps nested shared types (settings/variable values) into plain JSON. */
function toPlainValue(value: unknown): unknown {
  if (value instanceof Y.Text) return value.toString()
  if (value instanceof Y.Map || value instanceof Y.Array) return value.toJSON()
  return value
}

// ── Mutators (editor page; local Y transactions only) ─────────────────────────

function getWidgetMap(doc: Y.Doc, widgetId: string): Y.Map<unknown> | null {
  const wm = doc.getMap('widgets').get(widgetId)
  return wm instanceof Y.Map ? (wm as Y.Map<unknown>) : null
}

function setIfChanged(map: Y.Map<unknown>, key: string, value: unknown): void {
  if (map.get(key) !== value) map.set(key, value)
}

/**
 * Moves/resizes a widget. Each layout field is its own key so moving widget A
 * and resizing widget B merge instead of overwriting each other.
 */
export function updateWidgetLayout(collab: DashboardCollab, widgetId: string, layout: Widget['layout']): void {
  const wm = getWidgetMap(collab.doc, widgetId)
  if (!wm) return
  const current = wm.get('layout')
  const layoutMap = current instanceof Y.Map ? (current as Y.Map<unknown>) : new Y.Map<unknown>()
  collab.doc.transact(() => {
    if (!(current instanceof Y.Map)) wm.set('layout', layoutMap)
    setIfChanged(layoutMap, 'row', layout.row)
    setIfChanged(layoutMap, 'col', layout.col)
    setIfChanged(layoutMap, 'width', layout.width)
    setIfChanged(layoutMap, 'height', layout.height)
  })
}

/** Replaces a widget's SQL text; character-level edits bind to the Y.Text directly. */
export function setWidgetQuery(collab: DashboardCollab, widgetId: string, query: string): void {
  const wm = getWidgetMap(collab.doc, widgetId)
  if (!wm) return
  const existing = wm.get('query')
  if (existing instanceof Y.Text) {
    if (existing.toString() === query) return
    collab.doc.transact(() => {
      if (existing.length > 0) existing.delete(0, existing.length)
      if (query.length > 0) existing.insert(0, query)
    })
  } else if (query.length > 0) {
    collab.doc.transact(() => {
      wm.set('query', new Y.Text(query))
    })
  }
}

/** Stores the widget config as the document's JSON string (whole-value LWW). */
export function setWidgetConfig(collab: DashboardCollab, widgetId: string, config: Record<string, unknown>): void {
  const wm = getWidgetMap(collab.doc, widgetId)
  if (!wm) return
  const next = JSON.stringify(config)
  if (wm.get('config') === next) return
  collab.doc.transact(() => {
    wm.set('config', next)
  })
}

/** Sets the dashboard title (the hook debounces calls to this). */
export function setDashboardTitle(collab: DashboardCollab, title: string): void {
  const meta = collab.doc.getMap('meta')
  if (meta.get('title') === title) return
  collab.doc.transact(() => {
    meta.set('title', title)
  })
}
