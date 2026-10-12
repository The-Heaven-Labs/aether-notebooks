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

let appChartTheme: AppChartTheme | null = null

/** Sets the module-level override non-hook chart readers resolve. */
export function setAppChartTheme(theme: AppChartTheme | null): void {
  appChartTheme = theme
}

/**
 * Clears the override only while `theme` is still the active one: an
 * unmounting provider must not clobber a sibling provider that rendered in the
 * same commit (React renders the new tree before running the old tree's
 * cleanups).
 */
export function clearAppChartTheme(theme: AppChartTheme): void {
  if (appChartTheme === theme) appChartTheme = null
}

/** The active module-level override, or null when no provider is mounted. */
export function getAppChartTheme(): AppChartTheme | null {
  return appChartTheme
}
