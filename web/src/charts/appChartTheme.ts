import { createContext } from 'react'
import type { AppColorScheme } from '../utils/appTheme'

/**
 * The theme canvas charts render in.
 *
 * Charts draw to a `<canvas>`, so the scoped CSS custom properties the
 * `.app-theme` container sets are invisible to them. `AppThemeProvider`
 * resolves this object from the same inputs (dashboard theme + org branding +
 * the global scheme) and hands it to the chart layer two ways:
 *
 * - `AppChartThemeContext` for hook consumers (`useChartColors`,
 *   `useGroupBySeries`) — context propagation re-renders memoized chart
 *   subtrees when the theme changes while mounted.
 * - A module-level override for non-hook readers (`CHART_COLORS`, the tooltip
 *   and axis helpers), which resolve during a chart component's render.
 *
 * Kept in its own ECharts-free module so importing the context/setter never
 * pulls the chart renderer into a page's eager bundle (`OutputRenderer` lazy
 * loads `../charts`).
 */
export interface AppChartTheme {
  /** The scheme the app surface renders in. */
  scheme: AppColorScheme
  /** Validated palette override; absent means the scheme default. */
  palette?: string[]
  /** `--text-primary` override (already contrast-checked by resolveAppTheme). */
  text?: string
  /** `--border-light` override. */
  border?: string
  /** `--bg-card` override. */
  bgCard?: string
}

/** `null` outside an `AppThemeProvider` (DOM/global scheme applies). */
export const AppChartThemeContext = createContext<AppChartTheme | null>(null)

// Provider themes form a stack: nested providers layer over their ancestors
// and the innermost (last rendered) one is the active override. Entries are
// keyed by theme identity; `getAppChartTheme` exposes the top.
const appChartThemeStack: AppChartTheme[] = []

/**
 * Registers `theme` as the active module-level override.
 *
 * - Absent: pushed on top (a nested provider layers over its ancestors).
 * - Present: left in place — re-asserting must not reorder the stack, or an
 *   outer provider re-rendering would outrank a mounted inner one.
 *
 * `null` clears the whole stack.
 */
export function setAppChartTheme(theme: AppChartTheme | null): void {
  if (theme === null) {
    appChartThemeStack.length = 0
    return
  }
  if (!appChartThemeStack.includes(theme)) appChartThemeStack.push(theme)
}

/**
 * Removes `theme` by identity; the entry beneath it (if any) becomes active
 * again. An unmounting provider must not clobber a sibling provider that
 * rendered in the same commit (React renders the new tree before running the
 * old tree's cleanups), so removal is by identity rather than "clear all".
 */
export function clearAppChartTheme(theme: AppChartTheme): void {
  const idx = appChartThemeStack.indexOf(theme)
  if (idx !== -1) appChartThemeStack.splice(idx, 1)
}

/** The active module-level override (the innermost provider), or null. */
export function getAppChartTheme(): AppChartTheme | null {
  return appChartThemeStack.length > 0
    ? appChartThemeStack[appChartThemeStack.length - 1]
    : null
}
