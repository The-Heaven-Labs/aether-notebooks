import { useCallback, useMemo, useRef, useState } from 'react'
import type { CSSProperties } from 'react'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { customConfigFrom } from '../utils/customConfig'
import type { CustomBinding } from '../utils/customConfig'
import { SandboxedFrame } from './SandboxedFrame'
import { CustomBindingQuery } from './CustomBindingQuery'
import type { BindingResult } from './CustomBindingQuery'
import type { DashboardAppTheme, OrgBranding, Widget } from '../types'

interface CustomWidgetProps {
  html: string
  css: string
  js: string
  bindings: CustomBinding[]
  /** False when the org switch is off — renders a placeholder instead. */
  enabled?: boolean
  /** Every widget of the dashboard, for binding resolution. */
  widgets: Widget[]
  /** Values override for the public page (defaults when not live). */
  values?: Record<string, unknown>
  /** Execute-endpoint base; the public page passes `/api/v1/public/{token}`. */
  endpointBase?: string
  published?: boolean
  viewerConnectorId?: string | null
  /** Stored outputs per cell id, when the surface inlines them. */
  widgetsData?: Record<string, { outputs?: unknown[] | null } | undefined>
  /** False on public pages that are not live: data requests answer errors. */
  dataEnabled?: boolean
  theme?: DashboardAppTheme | null
  branding?: OrgBranding | null
}

/**
 * A custom component: the author's bundle in an allow-scripts opaque-origin
 * sandbox, fed through the validated postMessage bridge. Bindings resolve as
 * the viewer (query widgets through the execute pipeline, cell widgets from
 * stored outputs); a `request_data` is answered only when the viewer may see
 * the data — never with credentials or tokens.
 */
export function CustomWidget({ html, css, js, bindings, enabled = true, widgets, values, endpointBase, published, viewerConnectorId, widgetsData, dataEnabled = true, theme, branding }: CustomWidgetProps) {
  const context = useDashboardVariables()
  const effectiveValues = values ?? context.values
  const [results, setResults] = useState<Record<string, BindingResult>>({})
  const resultsRef = useRef(results)
  resultsRef.current = results

  const onResult = useCallback((result: BindingResult) => {
    setResults((prev) => (prev[result.key] === result ? prev : { ...prev, [result.key]: result }))
  }, [])

  const widgetsByID = useMemo(() => {
    const map = new Map<string, Widget>()
    for (const w of widgets) map.set(w.id, w)
    return map
  }, [widgets])

  const handleMessage = useCallback((message: { type: string; key?: string }, reply: (m: unknown) => void) => {
    if (message.type === 'ready') {
      reply({
        type: 'init',
        theme: {},
        variables: effectiveValues,
        bindings: bindings.map((b) => ({
          key: b.key,
          status: resultsRef.current[b.key]?.status === 'ready' ? 'ready' : 'unavailable',
        })),
      })
      return
    }
    if (message.type !== 'request_data' || !message.key) return
    if (!dataEnabled) {
      reply({ type: 'error', key: message.key, message: 'Live data is not enabled for this page.' })
      return
    }
    const result = resultsRef.current[message.key]
    if (!result || result.status !== 'ready') {
      reply({ type: 'error', key: message.key, message: result?.message ?? 'Data is not ready yet.' })
      return
    }
    reply({ type: 'data', key: message.key, result: result.result })
  }, [bindings, dataEnabled, effectiveValues])

  if (!enabled) return <div style={styles.empty}>Custom code is disabled for this organization.</div>
  if (!html.trim() && !css.trim() && !js.trim()) {
    return <div style={styles.empty}>Empty custom component — edit it in the dashboard editor.</div>
  }
  return (
    <div style={{ width: '100%', height: '100%' }}>
      {bindings.map((binding) => (
        <CustomBindingQuery
          key={binding.key}
          bindingKey={binding.key}
          widget={widgetsByID.get(binding.widget_id)}
          values={effectiveValues}
          viewerConnectorId={viewerConnectorId}
          endpointBase={endpointBase}
          published={published}
          cellOutputs={widgetsData?.[widgetsByID.get(binding.widget_id)?.cell_id ?? '']?.outputs}
          onResult={onResult}
        />
      ))}
      <SandboxedFrame html={html} css={css} js={js} onMessage={handleMessage} theme={theme} branding={branding} />
    </div>
  )
}

const styles: Record<string, CSSProperties> = {
  empty: { padding: 16, fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
}
