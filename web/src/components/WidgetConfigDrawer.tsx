import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X, Play, Loader2 } from 'lucide-react'
import { api } from '../api/client'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { useFocusTrap } from '../hooks/useFocusTrap'
import { ConnectorSelector } from './ConnectorSelector'
import { SqlEditor } from './SqlEditor'
import type { SqlEditorCollab } from './SqlEditor'
import { OutputRenderer } from './OutputRenderer'
import { normalizeChartConfig } from '../charts/normalizeChartConfig'
import type { ChartConfig } from '../charts/types'
import { withWidgetOverride } from '../charts/widgetChartConfig'
import type { Dashboard, DashboardVariableType, Widget, WidgetQueryResult } from '../types'

const styles: Record<string, React.CSSProperties> = {
  // The drawer covers the top bar (z-index 1550/1600), so it must sit above it:
  // otherwise the top bar hides the header and intercepts clicks on the close button.
  backdrop: {
    position: 'fixed',
    inset: 0,
    background: 'rgba(0,0,0,0.3)',
    zIndex: 1700,
  },
  drawer: {
    position: 'fixed',
    top: 0,
    right: 0,
    bottom: 0,
    width: 420,
    maxWidth: '100vw',
    background: 'var(--bg-card)',
    borderLeft: '1px solid var(--border)',
    display: 'flex',
    flexDirection: 'column',
    zIndex: 1701,
    boxShadow: 'var(--shadow-lg, -4px 0 16px rgba(0,0,0,0.2))',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '14px 16px',
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  title: { fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' },
  close: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    padding: '2px 4px',
    display: 'flex',
    alignItems: 'center',
  },
  body: {
    padding: 16,
    display: 'flex',
    flexDirection: 'column',
    gap: 10,
    overflowY: 'auto',
    flex: 1,
  },
  sectionLabel: {
    fontSize: 11,
    fontWeight: 700,
    color: 'var(--text-secondary)',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    marginTop: 4,
  },
  muted: { fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  error: { fontSize: 12, color: 'var(--danger, #d33)' },
  chipRow: { display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center' },
  chip: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: '2px 6px',
    color: 'var(--text-secondary)',
    background: 'var(--bg-input)',
  },
  defineBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--accent)',
    cursor: 'pointer',
    fontSize: 11,
    padding: 0,
  },
  runBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    alignSelf: 'flex-start',
    padding: '6px 12px',
    fontSize: 12,
    fontWeight: 600,
    background: 'var(--button-primary-bg)',
    color: 'var(--button-primary-text)',
    border: 'none',
    borderRadius: 4,
    cursor: 'pointer',
  },
  preview: {
    border: '1px solid var(--border)',
    borderRadius: 4,
    // A definite height as a flex column: chart views fill their flex parent,
    // and without it the chart collapsed to 0px in this non-flex preview box.
    // flexShrink keeps it from collapsing again on short viewports.
    height: 320,
    flexShrink: 0,
    minHeight: 240,
    display: 'flex',
    flexDirection: 'column',
    overflow: 'auto',
    background: 'var(--bg-card)',
  },
  select: {
    padding: '7px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    width: '100%',
  },
}

function detectedTokens(query: string): string[] {
  const out: string[] = []
  for (const match of query.matchAll(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g)) {
    if (!out.includes(match[1])) out.push(match[1])
  }
  return out
}

export function WidgetConfigDrawer({ dashboardId, dashboard, widget, onClose, onSaved, onDefineVariable, closeOnEscape = true, editingEnabled = true, docSynced = false }: {
  dashboardId: string
  dashboard: Dashboard
  widget: Widget
  onClose: () => void
  onSaved: () => void
  onDefineVariable?: (name: string, suggestedType?: DashboardVariableType) => void
  closeOnEscape?: boolean
  /** When false every mutating control is disabled (editing is paused). */
  editingEnabled?: boolean
  /** True once the dashboard document has synced; enables the live SQL binding. */
  docSynced?: boolean
}) {
  const isQuery = !!widget.connector_id && !!widget.query
  const canRun = dashboard.can_view_with_data !== false
  const { values } = useDashboardVariables()
  const [connectorId, setConnectorId] = useState<string | null>(widget.connector_id ?? null)
  const [query, setQuery] = useState(widget.query ?? '')
  const [widgetType, setWidgetType] = useState<Widget['type']>(widget.type)
  const [saveError, setSaveError] = useState<string | null>(null)

  const [running, setRunning] = useState(false)
  const [runResult, setRunResult] = useState<WidgetQueryResult | null>(null)
  const [runError, setRunError] = useState<string | null>(null)
  const [converting, setConverting] = useState(false)
  const [convertError, setConvertError] = useState<string | null>(null)
  const drawerRef = useRef<HTMLDivElement>(null)

  // Live document binding for the SQL editor. The runtime is imported on
  // demand (same lazy pattern as useDashboardDoc) and peeked without creating
  // a provider: when the page holds the dashboard document there is an entry,
  // otherwise the editor falls back to local state + REST.
  const [sqlCollab, setSqlCollab] = useState<SqlEditorCollab | null>(null)
  useEffect(() => {
    if (!docSynced) return
    let cancelled = false
    void import('./dashboardCollabRuntime').then((mod) => {
      if (cancelled) return
      const entry = mod.peekDashboardCollab(dashboardId)
      if (!entry) return
      setSqlCollab(prev => (
        prev && prev.collab === entry && prev.widgetId === widget.id ? prev : { collab: entry, widgetId: widget.id }
      ))
    })
    return () => { cancelled = true }
  }, [docSynced, dashboardId, widget.id])

  const lastSaved = useRef({ connector: widget.connector_id ?? '', query: widget.query ?? '' })

  // Debounced persistence of the connector (REST) and — without a bound
  // document — the SQL source. When the editor is bound to the shared
  // document the query text is persisted by the relay, so it must not be
  // sent over REST (the request would clobber concurrent doc edits); the
  // connector has no document mutator yet, so it always goes over REST and
  // the server merges it into the document.
  useEffect(() => {
    if (!isQuery) return
    const connectorChanged = (connectorId ?? '') !== lastSaved.current.connector
    const queryChanged = !sqlCollab && query !== lastSaved.current.query
    if (!connectorChanged && !queryChanged) return
    const timer = setTimeout(() => {
      const payload: Record<string, unknown> = {}
      if (connectorChanged) payload.connector_id = connectorId
      if (queryChanged) payload.query = query
      api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, payload)
        .then(() => {
          if (connectorChanged) lastSaved.current.connector = connectorId ?? ''
          if (queryChanged) lastSaved.current.query = query
          setSaveError(null)
          // Doc-bound writes propagate through the live document; only the
          // local-state fallback needs the REST refresh.
          if (!sqlCollab) onSaved()
        })
        .catch((e: unknown) => setSaveError(e instanceof Error ? e.message : 'Failed to save widget'))
    }, 600)
    return () => clearTimeout(timer)
  }, [connectorId, query, dashboardId, widget.id, isQuery, onSaved, sqlCollab])

  const definedVariables = useMemo(() => {
    const names = new Set<string>()
    for (const v of dashboard.settings?.variables ?? []) {
      names.add(v.name)
      // date_range variables expand server-side into {{name_start}}/{{name_end}},
      // so the derived tokens are defined too — never offer to redefine them.
      if (v.type === 'date_range') {
        names.add(`${v.name}_start`)
        names.add(`${v.name}_end`)
      }
    }
    return names
  }, [dashboard.settings?.variables])
  const references = useMemo(() => (isQuery ? detectedTokens(query) : []), [query, isQuery])

  const handleTypeChange = async (next: Widget['type']) => {
    if (!editingEnabled) return
    setWidgetType(next)
    try {
      await api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, { type: next })
      setSaveError(null)
      onSaved()
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : 'Failed to save widget type')
    }
  }

  const saveChartConfig = async (config: ChartConfig) => {
    if (!editingEnabled) return
    const next = isQuery ? config : withWidgetOverride(config)
    try {
      if (sqlCollab) {
        // The config lives in the shared document; the relay persists it and
        // no REST refresh is needed.
        const { setWidgetConfig } = await import('./dashboardCollabRuntime')
        setWidgetConfig(sqlCollab.collab, widget.id, next as unknown as Record<string, unknown>)
        setSaveError(null)
        return
      }
      await api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, { config: next })
      setSaveError(null)
      onSaved()
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : 'Failed to save chart config')
    }
  }

  const runQuery = async () => {
    setRunning(true)
    setRunError(null)
    try {
      const resp = await api.post<WidgetQueryResult>(`/api/v1/dashboards/${dashboardId}/execute`, {
        widget_id: widget.id,
        variables: values,
        bypass_cache: true,
      })
      setRunResult(resp)
    } catch (e) {
      setRunError(e instanceof Error ? e.message : 'Run failed')
    } finally {
      setRunning(false)
    }
  }

  const convertToQuery = async () => {
    if (!editingEnabled) return
    setConverting(true)
    setConvertError(null)
    try {
      await api.post(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}/convert-to-query`)
      onSaved()
      onClose()
    } catch (e) {
      setConvertError(e instanceof Error ? e.message : 'Failed to convert widget')
    } finally {
      setConverting(false)
    }
  }

  useEscapeToClose(onClose, closeOnEscape)
  useFocusTrap(drawerRef)

  return createPortal(
    <>
      <div data-testid="widget-drawer-backdrop" style={styles.backdrop} onClick={onClose} aria-hidden="true" />
      <div ref={drawerRef} style={styles.drawer} role="dialog" aria-modal="true" aria-label="Widget configuration" tabIndex={-1}>
        <div style={styles.header}>
          <span style={styles.title}>Widget configuration</span>
          <button type="button" style={styles.close} onClick={onClose} aria-label="Close widget configuration">
            <X size={15} />
          </button>
        </div>
        <div style={styles.body}>
          <span style={styles.sectionLabel}>Source</span>
          {isQuery ? (
            <>
              <ConnectorSelector value={connectorId} onChange={setConnectorId} disabled={!editingEnabled} />
              <SqlEditor value={query} onChange={setQuery} connectorType={undefined} collab={sqlCollab} editable={editingEnabled} />
              {references.length > 0 && (
                <div style={styles.chipRow}>
                  {references.map(name => (
                    <span key={name} style={styles.chip}>
                      {name}
                      {!definedVariables.has(name) && onDefineVariable && (
                        <button
                          type="button"
                          style={styles.defineBtn}
                          onClick={() => {
                            // A *_start/*_end token almost always belongs to a
                            // date_range variable; suggest the base name and type.
                            const suffix = name.endsWith('_start') ? '_start' : name.endsWith('_end') ? '_end' : ''
                            if (suffix) onDefineVariable(name.slice(0, -suffix.length), 'date_range')
                            else onDefineVariable(name)
                          }}
                        >
                          Define variable
                        </button>
                      )}
                    </span>
                  ))}
                </div>
              )}
              <button type="button" style={styles.runBtn} onClick={runQuery} disabled={running || !canRun}>
                {running ? <Loader2 size={12} style={{ animation: 'spin 1s linear infinite' }} /> : <Play size={12} />}
                {running ? 'Running…' : 'Run'}
              </button>
              {!canRun && (
                <div style={styles.muted} role="note">
                  You need view_with_data access to run queries. Ask a dashboard admin to grant it
                  in Permissions.
                </div>
              )}
              {runError && <div style={styles.error} role="alert">{runError}</div>}
              {runResult && (
                <div style={styles.preview}>
                  <OutputRenderer
                    outputs={runResult.outputs}
                    fixedView={widgetType === 'chart' ? 'chart' : 'table'}
                    chartConfig={normalizeChartConfig(widget.config)}
                    onChartConfigChange={saveChartConfig}
                  />
                </div>
              )}
            </>
          ) : (
            <>
              <div style={styles.muted}>
                Notebook cell widget. Convert it to a query widget to own its SQL and reference
                dashboard variables.
              </div>
              <button type="button" style={styles.runBtn} onClick={convertToQuery} disabled={converting || !editingEnabled}>
                {converting ? 'Converting…' : 'Convert to query widget'}
              </button>
              {convertError && <div style={styles.error} role="alert">{convertError}</div>}
            </>
          )}

          <label style={styles.sectionLabel} htmlFor="widget-type">Visualization</label>
          <select
            id="widget-type"
            aria-label="Widget type"
            style={styles.select}
            value={widgetType}
            disabled={!editingEnabled}
            onChange={(e) => { void handleTypeChange(e.target.value as Widget['type']) }}
          >
            <option value="table">Table</option>
            <option value="chart">Chart</option>
            <option value="metric">Metric</option>
            <option value="text">Text</option>
          </select>

          {saveError && <div style={styles.error} role="alert">{saveError}</div>}
        </div>
      </div>
    </>,
    document.body,
  )
}
