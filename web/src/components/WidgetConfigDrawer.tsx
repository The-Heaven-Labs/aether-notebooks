import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X, Play, Loader2 } from 'lucide-react'
import { api } from '../api/client'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { useFocusTrap } from '../hooks/useFocusTrap'
import { ConnectorSelector } from './ConnectorSelector'
import { SqlEditor } from './SqlEditor'
import { OutputRenderer } from './OutputRenderer'
import { MetricWidget } from './MetricWidget'
import { normalizeChartConfig } from '../charts/normalizeChartConfig'
import type { ChartConfig } from '../charts/types'
import { withWidgetOverride } from '../charts/widgetChartConfig'
import { metricConfigFrom, type MetricConfig } from '../utils/metric'
import type { ValueFormat } from '../utils/valueFormat'
import { textConfigFrom } from '../utils/textConfig'
import { appearanceFrom, type WidgetAppearance } from '../utils/widgetAppearance'
import { isTextPanel } from '../utils/widgetKind'
import type { Dashboard, DashboardVariableType, ResultSet, Widget, WidgetQueryResult } from '../types'

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
  input: {
    padding: '7px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    width: '100%',
  },
  textarea: {
    padding: '8px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    width: '100%',
    minHeight: 160,
    resize: 'vertical',
    fontFamily: 'var(--font-mono)',
  },
  field: { display: 'flex', flexDirection: 'column', gap: 4 },
  fieldLabel: { fontSize: 12, color: 'var(--text-secondary)' },
  fieldPair: { display: 'flex', gap: 8 },
  checkboxRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    fontSize: 13,
    color: 'var(--text-secondary)',
  },
}

function detectedTokens(query: string): string[] {
  const out: string[] = []
  for (const match of query.matchAll(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g)) {
    if (!out.includes(match[1])) out.push(match[1])
  }
  return out
}

/**
 * Text control that keeps a local draft and commits 600ms after the last
 * keystroke — the same debounce as the SQL source editor. The save callback
 * goes through a ref so parent re-renders never restart the timer mid-typing.
 */
function DebouncedTextInput({ label, value, onSave, listId, placeholder, multiline = false }: {
  label: string
  value: string
  onSave: (next: string) => void
  listId?: string
  placeholder?: string
  multiline?: boolean
}) {
  const [draft, setDraft] = useState(value)
  const saveRef = useRef(onSave)
  useEffect(() => {
    saveRef.current = onSave
  }, [onSave])
  useEffect(() => {
    if (draft === value) return
    const timer = setTimeout(() => saveRef.current(draft), 600)
    return () => clearTimeout(timer)
  }, [draft, value])
  if (multiline) {
    return (
      <textarea
        aria-label={label}
        style={styles.textarea}
        value={draft}
        placeholder={placeholder}
        onChange={e => setDraft(e.target.value)}
      />
    )
  }
  return (
    <input
      aria-label={label}
      type="text"
      style={styles.input}
      value={draft}
      list={listId}
      placeholder={placeholder}
      onChange={e => setDraft(e.target.value)}
    />
  )
}

export function WidgetConfigDrawer({ dashboardId, dashboard, widget, onClose, onSaved, onDefineVariable, closeOnEscape = true }: {
  dashboardId: string
  dashboard: Dashboard
  widget: Widget
  onClose: () => void
  onSaved: () => void
  onDefineVariable?: (name: string, suggestedType?: DashboardVariableType) => void
  closeOnEscape?: boolean
}) {
  const isQuery = !!widget.connector_id && !!widget.query
  const canRun = dashboard.can_view_with_data !== false
  const { values } = useDashboardVariables()
  const [connectorId, setConnectorId] = useState<string | null>(widget.connector_id ?? null)
  const [query, setQuery] = useState(widget.query ?? '')
  const [widgetType, setWidgetType] = useState<Widget['type']>(widget.type)
  const [saveError, setSaveError] = useState<string | null>(null)
  // The latest saved config. The `widget` prop is a snapshot, so merging every
  // field edit into this (instead of `widget.config`) keeps sequential saves
  // from clobbering each other while the drawer stays open.
  const [config, setConfig] = useState<Record<string, unknown>>(widget.config ?? {})
  const configRef = useRef<Record<string, unknown>>(widget.config ?? {})

  const [running, setRunning] = useState(false)
  const [runResult, setRunResult] = useState<WidgetQueryResult | null>(null)
  const [runError, setRunError] = useState<string | null>(null)
  const [converting, setConverting] = useState(false)
  const [convertError, setConvertError] = useState<string | null>(null)
  const drawerRef = useRef<HTMLDivElement>(null)

  const lastSaved = useRef({ connector: widget.connector_id ?? '', query: widget.query ?? '' })

  // Debounced persistence of the SQL source and connector.
  useEffect(() => {
    if (!isQuery) return
    if ((connectorId ?? '') === lastSaved.current.connector && query === lastSaved.current.query) return
    const timer = setTimeout(() => {
      const payload: Record<string, unknown> = { query }
      if (connectorId) payload.connector_id = connectorId
      api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, payload)
        .then(() => {
          lastSaved.current = { connector: connectorId ?? '', query }
          setSaveError(null)
          onSaved()
        })
        .catch((e: unknown) => setSaveError(e instanceof Error ? e.message : 'Failed to save widget'))
    }, 600)
    return () => clearTimeout(timer)
  }, [connectorId, query, dashboardId, widget.id, isQuery, onSaved])

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

  const metric = metricConfigFrom(config)
  const text = textConfigFrom(config)
  const appearance = appearanceFrom(config)
  // First table output of the last run: source for the metric preview and the
  // value-column suggestions. Null until a run has happened.
  const runResultSet = useMemo(() => {
    const data = runResult?.outputs.find(o => o.type === 'table')?.data as ResultSet | undefined
    return data && Array.isArray(data.columns) ? data : null
  }, [runResult])
  const metricColumns = runResultSet?.columns.map(c => c.name) ?? []

  const handleTypeChange = async (next: Widget['type']) => {
    setWidgetType(next)
    try {
      await api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, { type: next })
      setSaveError(null)
      onSaved()
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : 'Failed to save widget type')
    }
  }

  const putWidgetConfig = async (next: Record<string, unknown>) => {
    configRef.current = next
    setConfig(next)
    try {
      await api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, { config: next })
      setSaveError(null)
      onSaved()
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : 'Failed to save widget config')
    }
  }

  const saveWidgetConfig = (patch: Record<string, unknown>) =>
    putWidgetConfig({ ...configRef.current, ...patch })

  const saveChartConfig = (chartConfig: ChartConfig) => {
    if (isQuery) {
      // Query widgets store the chart config raw, alongside metric/text/
      // appearance blocks — merge so a chart edit keeps them.
      return putWidgetConfig({ ...configRef.current, ...chartConfig })
    }
    // Cell widgets keep the override-marker semantics; merging the current
    // config in means appearance/text blocks survive a chart edit too.
    return putWidgetConfig({ ...configRef.current, ...withWidgetOverride(chartConfig) })
  }

  const saveMetric = (patch: Partial<MetricConfig>) =>
    saveWidgetConfig({ metric: { ...metricConfigFrom(configRef.current), ...patch } })

  const saveAppearance = (patch: Partial<WidgetAppearance>) =>
    saveWidgetConfig({ appearance: { ...appearanceFrom(configRef.current), ...patch } })

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
              <ConnectorSelector value={connectorId} onChange={setConnectorId} />
              <SqlEditor value={query} onChange={setQuery} connectorType={undefined} />
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
                  {widgetType === 'metric' && runResultSet ? (
                    <MetricWidget result={runResultSet} config={metric} />
                  ) : (
                    <OutputRenderer
                      outputs={runResult.outputs}
                      fixedView={widgetType === 'chart' ? 'chart' : 'table'}
                      chartConfig={normalizeChartConfig(config)}
                      onChartConfigChange={saveChartConfig}
                    />
                  )}
                </div>
              )}
            </>
          ) : isTextPanel(widget) ? (
            <div style={styles.muted} role="note">
              Text widget — its content is authored as markdown in the Text section below.
              No notebook cell or query is needed.
            </div>
          ) : (
            <>
              <div style={styles.muted}>
                Notebook cell widget. Convert it to a query widget to own its SQL and reference
                dashboard variables.
              </div>
              <button type="button" style={styles.runBtn} onClick={convertToQuery} disabled={converting}>
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
            onChange={(e) => { void handleTypeChange(e.target.value as Widget['type']) }}
          >
            <option value="table">Table</option>
            <option value="chart">Chart</option>
            <option value="metric">Metric</option>
            <option value="text">Text</option>
          </select>

          {widgetType === 'metric' && (
            <>
              <span style={styles.sectionLabel}>Metric</span>
              <div style={styles.field}>
                <span style={styles.fieldLabel}>Value column</span>
                <DebouncedTextInput
                  label="Value column"
                  value={metric.value_column ?? ''}
                  listId={metricColumns.length > 0 ? 'metric-value-columns' : undefined}
                  placeholder="column"
                  onSave={v => void saveMetric({ value_column: v })}
                />
              </div>
              {metricColumns.length > 0 && (
                <datalist id="metric-value-columns">
                  {metricColumns.map(name => <option key={name} value={name} />)}
                </datalist>
              )}
              <div style={styles.field}>
                <span style={styles.fieldLabel}>Aggregation</span>
                <select
                  aria-label="Aggregation"
                  style={styles.select}
                  value={metric.aggregation ?? 'none'}
                  onChange={e => void saveMetric({ aggregation: e.target.value as MetricConfig['aggregation'] })}
                >
                  <option value="none">None (first row)</option>
                  <option value="sum">Sum</option>
                  <option value="avg">Average</option>
                  <option value="min">Min</option>
                  <option value="max">Max</option>
                  <option value="median">Median</option>
                  <option value="count">Count</option>
                  <option value="count_distinct">Count distinct</option>
                  <option value="first">First</option>
                </select>
              </div>
              <div style={styles.field}>
                <span style={styles.fieldLabel}>Label</span>
                <DebouncedTextInput
                  label="Label"
                  value={metric.label ?? ''}
                  placeholder="Total revenue"
                  onSave={v => void saveMetric({ label: v })}
                />
              </div>
              <div style={styles.field}>
                <span style={styles.fieldLabel}>Format</span>
                <select
                  aria-label="Format"
                  style={styles.select}
                  value={metric.format?.style ?? 'number'}
                  onChange={e => void saveMetric({ format: { ...metric.format, style: e.target.value as ValueFormat['style'] } })}
                >
                  <option value="number">Number</option>
                  <option value="currency">Currency</option>
                  <option value="percent">Percent</option>
                </select>
              </div>
              {metric.format?.style === 'currency' && (
                <div style={styles.field}>
                  <span style={styles.fieldLabel}>Currency</span>
                  <DebouncedTextInput
                    label="Currency"
                    value={metric.format?.currency ?? ''}
                    placeholder="USD"
                    onSave={v => void saveMetric({ format: { ...metric.format, currency: v } })}
                  />
                </div>
              )}
              <div style={styles.field}>
                <span style={styles.fieldLabel}>Decimals</span>
                <input
                  aria-label="Decimals"
                  type="number"
                  min={0}
                  style={styles.input}
                  value={metric.format?.decimals ?? ''}
                  onChange={e => {
                    const raw = e.target.value
                    const parsed = raw === '' ? undefined : Number(raw)
                    const decimals = parsed !== undefined && Number.isFinite(parsed) ? Math.max(0, Math.trunc(parsed)) : undefined
                    void saveMetric({ format: { ...metric.format, decimals } })
                  }}
                />
              </div>
              <div style={styles.checkboxRow}>
                <input
                  type="checkbox"
                  aria-label="Compact"
                  checked={metric.format?.compact ?? false}
                  onChange={e => void saveMetric({ format: { ...metric.format, compact: e.target.checked } })}
                />
                <span>Compact (1.2M)</span>
              </div>
              <div style={styles.field}>
                <span style={styles.fieldLabel}>Comparison column</span>
                <DebouncedTextInput
                  label="Comparison column"
                  value={metric.comparison?.column ?? ''}
                  placeholder="revenue_prev"
                  onSave={v => void saveMetric({ comparison: { ...metric.comparison, column: v } })}
                />
              </div>
              <div style={styles.field}>
                <span style={styles.fieldLabel}>Comparison mode</span>
                <select
                  aria-label="Comparison mode"
                  style={styles.select}
                  value={metric.comparison?.mode ?? 'percent'}
                  onChange={e => void saveMetric({ comparison: { ...metric.comparison, mode: e.target.value as 'delta' | 'percent' } })}
                >
                  <option value="percent">Percent</option>
                  <option value="delta">Delta</option>
                </select>
              </div>
              <span style={styles.fieldLabel}>Sparkline</span>
              <div style={styles.fieldPair}>
                <div style={{ ...styles.field, flex: 1 }}>
                  <span style={styles.fieldLabel}>X column</span>
                  <DebouncedTextInput
                    label="Sparkline x column"
                    value={metric.sparkline?.x_column ?? ''}
                    onSave={v => void saveMetric({ sparkline: { ...metric.sparkline, x_column: v } })}
                  />
                </div>
                <div style={{ ...styles.field, flex: 1 }}>
                  <span style={styles.fieldLabel}>Y column</span>
                  <DebouncedTextInput
                    label="Sparkline y column"
                    value={metric.sparkline?.y_column ?? ''}
                    onSave={v => void saveMetric({ sparkline: { ...metric.sparkline, y_column: v } })}
                  />
                </div>
              </div>
            </>
          )}

          {widgetType === 'text' && (
            <>
              <span style={styles.sectionLabel}>Text</span>
              <DebouncedTextInput
                label="Markdown"
                value={text.markdown}
                multiline
                placeholder={'## Heading\n\nMarkdown text…'}
                onSave={markdown => void saveWidgetConfig({ text: { markdown } })}
              />
              <div style={styles.muted}>
                {'{{variable}}'} tokens are interpolated with dashboard variable values for display.
              </div>
            </>
          )}

          <span style={styles.sectionLabel}>Appearance</span>
          <div style={styles.field}>
            <span style={styles.fieldLabel}>Background</span>
            <select
              aria-label="Background"
              style={styles.select}
              value={appearance.background}
              onChange={e => void saveAppearance({ background: e.target.value as WidgetAppearance['background'] })}
            >
              <option value="card">Card</option>
              <option value="transparent">Transparent</option>
            </select>
          </div>
          <div style={styles.checkboxRow}>
            <input
              type="checkbox"
              aria-label="Border"
              checked={appearance.border}
              disabled={appearance.background === 'transparent'}
              onChange={e => void saveAppearance({ border: e.target.checked })}
            />
            <span>Border</span>
          </div>
          {appearance.background === 'transparent' && (
            <div style={styles.muted}>Transparent widgets never draw a border.</div>
          )}
          <div style={styles.field}>
            <span style={styles.fieldLabel}>Padding</span>
            <select
              aria-label="Padding"
              style={styles.select}
              value={appearance.padding}
              onChange={e => void saveAppearance({ padding: e.target.value as WidgetAppearance['padding'] })}
            >
              <option value="none">None</option>
              <option value="sm">Small</option>
              <option value="md">Medium</option>
            </select>
          </div>
          <div style={styles.field}>
            <span style={styles.fieldLabel}>Align</span>
            <select
              aria-label="Align"
              style={styles.select}
              value={appearance.align}
              onChange={e => void saveAppearance({ align: e.target.value as WidgetAppearance['align'] })}
            >
              <option value="left">Left</option>
              <option value="center">Center</option>
              <option value="right">Right</option>
            </select>
          </div>

          {saveError && <div style={styles.error} role="alert">{saveError}</div>}
        </div>
      </div>
    </>,
    document.body,
  )
}
