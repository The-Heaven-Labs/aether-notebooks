import type { DashboardTab, Widget } from '../types'

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
export function widgetTabId(widget: Widget, tabs: DashboardTab[]): string | null {
  if (tabs.length === 0) return null
  const tab = widget.layout?.tab
  return tab && tabs.some((t) => t.id === tab) ? tab : tabs[0].id
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
