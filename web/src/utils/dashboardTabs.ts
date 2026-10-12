import type { DashboardTab, Widget } from '../types'

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
 * The widgets to render for a tab. With no tabs configured every widget
 * renders (and the tab bar stays hidden). A widget whose `layout.tab` is
 * empty or names a tab that no longer exists falls into the first tab, so a
 * deleted tab never strands its widgets off-screen.
 */
export function widgetsInTab(
  widgets: Widget[],
  tabs: DashboardTab[],
  requested: string | null | undefined,
): Widget[] {
  if (tabs.length === 0) return widgets
  const active = activeTabId(tabs, requested)
  const known = new Set(tabs.map((t) => t.id))
  return widgets.filter((w) => {
    const tab = w.layout?.tab
    const effective = tab && known.has(tab) ? tab : tabs[0].id
    return effective === active
  })
}
