import type { DashboardTab, Widget } from '../types'

/** The identity a chapter is referenced by; titles are presentation only. */
export interface DashboardTabLike {
  id: string
}

/**
 * Stable identity for a dashboard without tabs, so callers can fall back to
 * it without creating a fresh array on every render (which would defeat any
 * memo keyed on the tab list).
 */
export const NO_TABS: DashboardTab[] = []

/**
 * The tab to show: the requested id when it names an existing tab, else the
 * first tab, else null when the dashboard defines no tabs at all. An unknown
 * `?tab=` value therefore lands on the first chapter instead of an empty page.
 */
export function activeTabId(tabs: DashboardTab[], requested: string | null | undefined): string | null {
  if (tabs.length === 0) return null
  if (requested && tabs.some((t) => t.id === requested)) return requested
  return tabs[0].id
}

/**
 * The tab a widget renders under: its own `layout.tab` when it names a known
 * tab, else the first tab — a widget whose tab is empty or names a tab that
 * no longer exists falls back to the first tab, so a deleted tab never
 * strands its widgets off-screen. Null when the dashboard defines no tabs.
 */
export function widgetTabId(widget: Widget, tabs: readonly DashboardTabLike[]): string | null {
  if (tabs.length === 0) return null
  const tab = widget.layout?.tab
  return tab && tabs.some((t) => t.id === tab) ? tab : tabs[0].id
}

/**
 * A query-string fragment for linking to a specific chapter. Returns `?tab=<id>`
 * for a non-empty id, otherwise an empty string so All / no-tab dashboards do
 * not leave a stale param in the URL.
 */
export function tabQueryString(tabId: string | null | undefined): string {
  if (!tabId) return ''
  return `?tab=${encodeURIComponent(tabId)}`
}

/**
 * Deterministic render order for the editor's All view: chapter order first
 * (widgets with no/invalid `tab` fall into the first chapter's bucket via
 * `widgetTabId`'s fallback), then `(layout.row, layout.col)`. The sort is stable
 * so widgets sharing a position keep their document order across renders and
 * doc syncs. Non-mutating; with no tabs everything lands in a single bucket.
 */
export function orderWidgetsForEditor(widgets: Widget[], tabs: DashboardTab[]): Widget[] {
  const tabIndex = (widget: Widget): number => {
    const index = tabs.findIndex((t) => t.id === widgetTabId(widget, tabs))
    // No tabs (or no matching bucket) → the first bucket.
    return index < 0 ? 0 : index
  }
  return [...widgets].sort(
    (a, b) =>
      tabIndex(a) - tabIndex(b) ||
      a.layout.row - b.layout.row ||
      a.layout.col - b.layout.col,
  )
}

/**
 * The widgets that belong to one tab. With no tabs configured every widget
 * matches (and the tab bar stays hidden); an unknown requested id resolves to
 * the first tab, mirroring `activeTabId`.
 */
export function widgetsInTab(
  widgets: Widget[],
  tabs: DashboardTab[],
  requested: string | null | undefined,
): Widget[] {
  if (tabs.length === 0) return widgets
  const active = activeTabId(tabs, requested)
  return widgets.filter((w) => widgetTabId(w, tabs) === active)
}
