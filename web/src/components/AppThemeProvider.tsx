import { createContext, useContext, useLayoutEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { DashboardAppTheme, OrgBranding } from '../types'
import { resolveAppTheme } from '../utils/appTheme'
import type { AppColorScheme, ResolvedAppTheme } from '../utils/appTheme'
import {
  AppChartThemeContext,
  clearAppChartTheme,
  setAppChartTheme,
} from '../charts/appChartTheme'
import type { AppChartTheme } from '../charts/appChartTheme'

interface AppThemeProviderProps {
  /** Dashboard `settings.app.theme`; absent keeps the global theme. */
  theme?: DashboardAppTheme | null
  /** Org-level branding fallback (`org_branding` on the response). */
  branding?: OrgBranding | null
  children: ReactNode
}

/** Resolved theme for consumers that cannot read CSS variables (iframes). */
const AppThemeContext = createContext<ResolvedAppTheme | null>(null)

/** Null outside an AppThemeProvider (editor chrome / global theme applies). */
export function useAppTheme(): ResolvedAppTheme | null {
  return useContext(AppThemeContext)
}

function readGlobalScheme(): AppColorScheme {
  return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light'
}

/** Mirrors `useChartColors`' observer: `color_scheme: 'auto'` follows the app. */
function useGlobalColorScheme(): AppColorScheme {
  const [scheme, setScheme] = useState<AppColorScheme>(readGlobalScheme)

  useLayoutEffect(() => {
    const el = document.documentElement
    const update = () => setScheme(readGlobalScheme())
    const observer = new MutationObserver(update)
    observer.observe(el, { attributes: true, attributeFilter: ['data-theme'] })
    return () => observer.disconnect()
  }, [])

  return scheme
}

/**
 * Applies a dashboard's resolved theme to the app surface only: a `.app-theme`
 * container carrying the scoped CSS variables (theme.css), plus the matching
 * chart theme (canvas charts cannot read CSS variables).
 *
 * Wrap the whole page surface — the container paints the canvas and sets
 * `min-height: 100vh`, so wrapping only `<main>` would add a scrollbar of
 * header/footer height on pages that have one (e.g. the public footer).
 */
export function AppThemeProvider({ theme, branding, children }: AppThemeProviderProps) {
  const globalScheme = useGlobalColorScheme()

  // Keyed by content, not identity: callers rebuild `settings`/branding objects
  // on every render, and an identity-based memo would churn the context value
  // and re-render every memoized chart consumer.
  const themeKey = JSON.stringify(theme ?? null)
  const brandingKey = JSON.stringify(branding ?? null)
  const resolved = useMemo(
    () => resolveAppTheme(theme, branding, globalScheme),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [themeKey, brandingKey, globalScheme],
  )

  const chartTheme = useMemo<AppChartTheme>(
    () => ({
      scheme: resolved.scheme,
      palette: resolved.chartPalette,
      text: resolved.vars['--text-primary'],
      border: resolved.vars['--border-light'],
      bgCard: resolved.vars['--bg-card'],
    }),
    [resolved],
  )

  // Chart components resolve `CHART_COLORS` during render and module readers
  // cannot consume context, so the module-level override must already be
  // current when children render — a layout effect runs too late for the same
  // pass. The write is idempotent and derived purely from props; the layout
  // effect below re-asserts it (StrictMode's simulated remount) and clears this
  // provider's stack entry on unmount, re-exposing an outer provider's theme.
  setAppChartTheme(chartTheme)
  useLayoutEffect(() => {
    setAppChartTheme(chartTheme)
    return () => clearAppChartTheme(chartTheme)
  }, [chartTheme])

  return (
    <AppChartThemeContext.Provider value={chartTheme}>
      <AppThemeContext.Provider value={resolved}>
        <div className="app-theme" data-app-scheme={resolved.scheme} style={resolved.vars}>
          {children}
        </div>
      </AppThemeContext.Provider>
    </AppChartThemeContext.Provider>
  )
}
