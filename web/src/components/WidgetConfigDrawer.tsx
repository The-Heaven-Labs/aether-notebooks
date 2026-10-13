import { useEffect, useMemo, useRef, useState, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { X, Play, Loader2, Plus } from 'lucide-react'
import { api } from '../api/client'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { useFocusTrap } from '../hooks/useFocusTrap'
import { ConnectorSelector } from './ConnectorSelector'
import { SqlEditor } from './SqlEditor'
import type { SqlEditorCollab } from './SqlEditor'
import { OutputRenderer } from './OutputRenderer'
import { MetricWidget } from './MetricWidget'
import { normalizeChartConfig } from '../charts/normalizeChartConfig'
import type { ChartConfig } from '../charts/types'
import { withWidgetOverride } from '../charts/widgetChartConfig'
import { metricConfigFrom, type MetricConfig } from '../utils/metric'
import type { ValueFormat } from '../utils/valueFormat'
import { tableConfigFrom } from '../utils/tableConfig'
import { textConfigFrom } from '../utils/textConfig'
import { htmlConfigFrom } from '../utils/htmlConfig'
import { customConfigFrom } from '../utils/customConfig'
import type { CustomBinding } from '../utils/customConfig'
import { appearanceFrom, type WidgetAppearance } from '../utils/widgetAppearance'
import { isHTMLPanel, isQueryWidget, isTextPanel } from '../utils/widgetKind'
import { HtmlWidget } from './HtmlWidget'
import { SandboxedFrame } from './SandboxedFrame'
import { CodeMirrorField } from './CodeMirrorField'
import { fieldHelpFor } from '../app-builder/fieldHelp'
import { HelpTip } from './HelpTip'
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
  bindingRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    marginBottom: 6,
  },
  bindingRemove: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 4,
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'transparent',
    color: 'var(--text-muted)',
    cursor: 'pointer',
  },
  bindingAdd: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    padding: '4px 8px',
    fontSize: 12,
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'transparent',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
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

// Currency fields accept free text; trim + uppercase before persisting so
// obvious mistakes ("usd ") never reach the config. Invalid codes are dropped
// by tableConfigFrom and tolerated by formatValue's fallback.
function normalizeCurrency(value: string): string {
  return value.trim().toUpperCase()
}

/**
 * A field label with an optional HelpTip from the shared field-help source.
 * Progressive disclosure: only fields whose values need an example get the
 * trigger; the label itself stays the same.
 */
function FieldLabel({ helpKey, section = false, children }: { helpKey?: string; section?: boolean; children: React.ReactNode }) {
  const help = helpKey ? fieldHelpFor(helpKey) : undefined
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center' }}>
      <span style={section ? styles.sectionLabel : styles.fieldLabel}>{children}</span>
      {help && <HelpTip help={help} />}
    </span>
  )
}

/**
 * Text control that keeps a local draft and commits 600ms after the last
 * keystroke — the same debounce as the SQL source editor. The save callback
 * goes through a ref so parent re-renders never restart the timer mid-typing.
 * An uncommitted draft is flushed once on unmount so closing the drawer (or
 * hiding the section) inside the debounce window never drops the edit's tail.
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
  const draftRef = useRef(value)
  const valueRef = useRef(value)
  // Last draft handed to onSave — guards the unmount flush against saving the
  // same draft twice when the debounce timer already fired.
  const lastSentRef = useRef(value)
  const saveRef = useRef(onSave)
  useEffect(() => {
    saveRef.current = onSave
  }, [onSave])
  useEffect(() => {
    valueRef.current = value
  }, [value])
  useEffect(() => {
    if (draft === value) return
    const timer = setTimeout(() => {
      lastSentRef.current = draft
      saveRef.current(draft)
    }, 600)
    return () => clearTimeout(timer)
  }, [draft, value])
  useEffect(() => () => {
    const pending = draftRef.current
    if (pending !== valueRef.current && pending !== lastSentRef.current) {
      lastSentRef.current = pending
      saveRef.current(pending)
    }
  }, [])
  const handleChange = (next: string) => {
    draftRef.current = next
    setDraft(next)
  }
  if (multiline) {
    return (
      <textarea
        aria-label={label}
        style={styles.textarea}
        value={draft}
        placeholder={placeholder}
        onChange={e => handleChange(e.target.value)}
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
      onChange={e => handleChange(e.target.value)}
    />
  )
}

/**
 * Chart edits merge into the widget config. Query widgets store the chart
 * config raw; cell widgets keep the `withWidgetOverride` marker semantics.
 * Exported so the cell-widget branch stays covered by tests — its chart UI
 * lives in the editor grid (WidgetContent), not in this drawer.
 */
// eslint-disable-next-line react-refresh/only-export-components -- pure helper; a separate file is out of this task's scope
export function mergeChartConfigInto(
  config: Record<string, unknown>,
  chartConfig: ChartConfig,
  isQuery: boolean,
): Record<string, unknown> {
  return isQuery
    ? { ...config, ...chartConfig }
    : { ...config, ...withWidgetOverride(chartConfig) }
}

export function WidgetConfigDrawer({ dashboardId, dashboard, widget, onClose, onSaved, onDefineVariable, closeOnEscape = true, editingEnabled = true, docSynced = false, htmlPanelsEnabled = true, customCodeEnabled = false }: {
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
  /** False when the org switch is off — no html option, editors, or preview. */
  htmlPanelsEnabled?: boolean
  /** False when the org switch is off (default) — no custom option or authoring. */
  customCodeEnabled?: boolean
}) {
  const isQuery = isQueryWidget(widget)
  const canRun = dashboard.can_view_with_data !== false
  const { values } = useDashboardVariables()
  const [connectorId, setConnectorId] = useState<string | null>(widget.connector_id ?? null)
  const [query, setQuery] = useState(widget.query ?? '')
  const [widgetType, setWidgetType] = useState<Widget['type']>(widget.type)
  // Placement: "" is the first tab (layout.tab absent). The select's value is
  // derived per render so a tab deleted while the drawer is open falls back to
  // the default instead of showing a stale id.
  const tabs = dashboard.settings?.tabs ?? []
  const [tab, setTab] = useState(widget.layout?.tab ?? '')
  const selectedTab = tab && tabs.some((t) => t.id === tab) ? tab : ''
  const [saveError, setSaveError] = useState<string | null>(null)
  // The latest saved config. The `widget` prop is a snapshot, so merging every
  // field edit into this (instead of `widget.config`) keeps sequential saves
  // from clobbering each other while the drawer stays open.
  const [config, setConfig] = useState<Record<string, unknown>>(widget.config ?? {})
  const configRef = useRef<Record<string, unknown>>(widget.config ?? {})
  // Config PUTs are serialized through this chain (see putWidgetConfig): each
  // save queues behind the previous request so a slow write cannot land after
  // a newer one and silently drop the newer fields.
  const saveChain = useRef<Promise<void>>(Promise.resolve())

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
  // True once the live-document lookup has settled. Until then the SQL editor
  // stays read-only: typing before the binding attaches would be reverted when
  // y-codemirror adopts the shared text.
  const [collabLookupDone, setCollabLookupDone] = useState(false)
  const collabPending = docSynced && !collabLookupDone
  useEffect(() => {
    if (!docSynced) {
      setCollabLookupDone(false)
      return
    }
    let cancelled = false
    void import('./dashboardCollabRuntime')
      .then((mod) => {
        if (cancelled) return
        const entry = mod.peekDashboardCollab(dashboardId)
        setSqlCollab(prev => {
          if (!entry) return null
          return prev && prev.collab === entry && prev.widgetId === widget.id
            ? prev
            : { collab: entry, widgetId: widget.id }
        })
      })
      .catch(() => { /* fall back to local editing + REST */ })
      .finally(() => {
        if (!cancelled) setCollabLookupDone(true)
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

  const metric = metricConfigFrom(config)
  const text = textConfigFrom(config)
  const html = htmlConfigFrom(config)
  const custom = customConfigFrom(config)
  const appearance = appearanceFrom(config)
  const tableColumns = tableConfigFrom(config).columns
  // First table output of the last run: source for the metric preview, the
  // value-column suggestions, and the table column formatting list. Null until
  // a run has happened.
  const runResultSet = useMemo(() => {
    const data = runResult?.outputs.find(o => o.type === 'table')?.data as ResultSet | undefined
    return data && Array.isArray(data.columns) ? data : null
  }, [runResult])
  const resultColumns = runResultSet?.columns.map(c => c.name) ?? []

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

  const putWidgetConfig = (next: Record<string, unknown>) => {
    if (!editingEnabled) return Promise.resolve()
    configRef.current = next
    setConfig(next)
    if (sqlCollab) {
      // The config lives in the shared document; the relay persists it and
      // no REST refresh is needed.
      return import('./dashboardCollabRuntime').then(({ setWidgetConfig }) => {
        setWidgetConfig(sqlCollab.collab, widget.id, configRef.current)
        setSaveError(null)
      })
    }
    // Append to the chain and send the latest merged config at execution time,
    // so a queued save still writes the freshest payload (last-write-wins).
    // The catch keeps one failed request from breaking every later save, and
    // leaves no rejection unhandled.
    saveChain.current = saveChain.current
      .then(() => api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, { config: configRef.current }))
      .then(() => {
        setSaveError(null)
        onSaved()
      })
      .catch((e: unknown) => {
        setSaveError(e instanceof Error ? e.message : 'Failed to save widget config')
      })
    return saveChain.current
  }

  const saveWidgetConfig = (patch: Record<string, unknown>) =>
    putWidgetConfig({ ...configRef.current, ...patch })

  const saveChartConfig = (chartConfig: ChartConfig) =>
    putWidgetConfig(mergeChartConfigInto(configRef.current, chartConfig, isQuery))

  const saveMetric = (patch: Partial<MetricConfig>) =>
    saveWidgetConfig({ metric: { ...metricConfigFrom(configRef.current), ...patch } })

  const saveAppearance = (patch: Partial<WidgetAppearance>) =>
    saveWidgetConfig({ appearance: { ...appearanceFrom(configRef.current), ...patch } })

  // Custom-component bindings authoring: rows serialize straight into
  // config.bindings; the server validates key charset, duplicates, and
  // same-dashboard references. The picker lists the dashboard's other widgets.
  const bindableWidgets = (dashboard.widgets ?? []).filter(w => w.id !== widget.id)
  const saveBindings = (bindings: CustomBinding[]) => void saveWidgetConfig({ bindings })
  const bindingTargetLabel = (w: Widget) => {
    const title = typeof w.config?.title === 'string' ? w.config.title : ''
    return title || `${w.type} ${w.id.slice(0, 8)}`
  }
  const addBinding = () => {
    const first = bindableWidgets[0]
    if (!first) return
    let n = custom.bindings.length + 1
    while (custom.bindings.some(b => b.key === `binding${n}`)) n++
    saveBindings([...custom.bindings, { key: `binding${n}`, widget_id: first.id }])
  }

  // Writes the table block, preserving any sibling keys under it: only columns
  // with a format are listed, so setting a column back to Default removes its
  // entry — dropping the columns list, and the whole table key when it has no
  // other content. Entries are edited as an array (never by record assignment)
  // so prototype-named columns survive.
  const saveTableColumn = (name: string, format: ValueFormat | null) => {
    const entries = Object.entries(tableConfigFrom(configRef.current).columns)
    const idx = entries.findIndex(([column]) => column === name)
    if (format) {
      if (idx >= 0) entries[idx] = [name, format]
      else entries.push([name, format])
    } else if (idx >= 0) {
      entries.splice(idx, 1)
    } else {
      return
    }
    const current = configRef.current
    const existing = current.table
    const table: Record<string, unknown> = existing && typeof existing === 'object' ? { ...(existing as Record<string, unknown>) } : {}
    if (entries.length > 0) {
      table.columns = entries.map(([column, columnFormat]) => ({ name: column, format: columnFormat }))
    } else {
      delete table.columns
    }
    const next = { ...current }
    if (Object.keys(table).length > 0) next.table = table
    else delete next.table
    void putWidgetConfig(next)
  }

  const saveTableColumnStyle = (name: string, style: ValueFormat['style'] | 'default') => {
    if (style === 'default') {
      saveTableColumn(name, null)
      return
    }
    const next: ValueFormat = { ...tableConfigFrom(configRef.current).columns[name], style }
    // A currency code only means something for the currency style.
    if (style !== 'currency') delete next.currency
    saveTableColumn(name, next)
  }

  // Placement lives in the widget's layout: doc-bound drawers write the tab
  // straight to the shared document (the relay persists it); the unbound
  // fallback sends the full layout so the grid fields survive the PUT.
  const saveTab = (next: string) => {
    if (!editingEnabled) return
    setTab(next)
    if (sqlCollab) {
      void import('./dashboardCollabRuntime').then(({ setWidgetTab }) => {
        setWidgetTab(sqlCollab.collab, widget.id, next)
        setSaveError(null)
      }).catch((e: unknown) => setSaveError(e instanceof Error ? e.message : 'Failed to save widget tab'))
      return
    }
    api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, {
      layout: { ...widget.layout, tab: next },
    }).then(() => {
      setSaveError(null)
      onSaved()
    }).catch((e: unknown) => setSaveError(e instanceof Error ? e.message : 'Failed to save widget tab'))
  }

  const runQuery = useCallback(async (bypassCache: boolean) => {
    setRunning(true)
    setRunError(null)
    try {
      const resp = await api.post<WidgetQueryResult>(`/api/v1/dashboards/${dashboardId}/execute`, {
        widget_id: widget.id,
        variables: values,
        bypass_cache: bypassCache,
      })
      setRunResult(resp)
    } catch (e) {
      setRunError(e instanceof Error ? e.message : 'Run failed')
    } finally {
      setRunning(false)
    }
  }, [dashboardId, widget.id, values])

  // Preview on open: the preview — and with it the Configure chart button —
  // is the point of the drawer, so run once when it opens instead of making
  // the author click Run first. This run goes through the cache (the
  // dashboard's own widgets usually just executed), so it is instant; the
  // Run button still bypasses the cache for fresh data.
  const autoRan = useRef(false)
  useEffect(() => {
    if (autoRan.current) return
    if (!isQuery || !canRun || runResult || running) return
    autoRan.current = true
    // Deferred to a microtask: runQuery sets `running` synchronously, and
    // setting state during effect execution is what the lint rule guards.
    void Promise.resolve().then(() => runQuery(false))
  }, [isQuery, canRun, runResult, running, runQuery])

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
              <SqlEditor
                value={query}
                onChange={setQuery}
                connectorType={undefined}
                collab={sqlCollab}
                editable={editingEnabled && !collabPending}
                onCollabUnavailable={() => setSqlCollab(null)}
              />
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
              <button type="button" style={styles.runBtn} onClick={() => void runQuery(true)} disabled={running || !canRun}>
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
                      columnFormats={tableColumns}
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
          ) : isHTMLPanel(widget) ? (
            <div style={styles.muted} role="note">
              HTML panel — its content is authored in the Panel HTML and Panel CSS sections below.
              No notebook cell or query is needed.
            </div>
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
            {htmlPanelsEnabled && <option value="html">HTML panel</option>}
            {customCodeEnabled && <option value="custom">Custom component</option>}
          </select>

          {/* Placement is a chapter control: nothing to choose without tabs. */}
          {tabs.length > 0 && (
            <>
              <label style={styles.sectionLabel} htmlFor="widget-tab">Placement</label>
              <select
                id="widget-tab"
                aria-label="Placement"
                style={styles.select}
                value={selectedTab}
                disabled={!editingEnabled}
                onChange={(e) => saveTab(e.target.value)}
              >
                <option value="">First tab (default)</option>
                {tabs.map((t) => (
                  <option key={t.id} value={t.id}>{t.title}</option>
                ))}
              </select>
            </>
          )}

          {widgetType === 'metric' && (
            <>
              <span style={styles.sectionLabel}>Metric</span>
              <div style={styles.field}>
                <FieldLabel helpKey="metric.value_column">Value column</FieldLabel>
                <DebouncedTextInput
                  label="Value column"
                  value={metric.value_column ?? ''}
                  listId={resultColumns.length > 0 ? 'metric-value-columns' : undefined}
                  placeholder="column"
                  onSave={v => void saveMetric({ value_column: v })}
                />
              </div>
              {resultColumns.length > 0 && (
                <datalist id="metric-value-columns">
                  {resultColumns.map(name => <option key={name} value={name} />)}
                </datalist>
              )}
              <div style={styles.field}>
                <FieldLabel helpKey="metric.aggregation">Aggregation</FieldLabel>
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
                <FieldLabel helpKey="metric.label">Label</FieldLabel>
                <DebouncedTextInput
                  label="Label"
                  value={metric.label ?? ''}
                  placeholder="Total revenue"
                  onSave={v => void saveMetric({ label: v })}
                />
              </div>
              <div style={styles.field}>
                <FieldLabel helpKey="metric.format.style">Format</FieldLabel>
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
                    onSave={v => void saveMetric({ format: { ...metric.format, currency: normalizeCurrency(v) } })}
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
                  id="widget-metric-compact"
                  type="checkbox"
                  aria-label="Compact"
                  checked={metric.format?.compact ?? false}
                  onChange={e => void saveMetric({ format: { ...metric.format, compact: e.target.checked } })}
                />
                <label htmlFor="widget-metric-compact">Compact (1.2M)</label>
              </div>
              <div style={styles.field}>
                <FieldLabel helpKey="metric.comparison.column">Comparison column</FieldLabel>
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
                  <FieldLabel helpKey="metric.sparkline.x_column">X column</FieldLabel>
                  <DebouncedTextInput
                    label="Sparkline x column"
                    value={metric.sparkline?.x_column ?? ''}
                    onSave={v => void saveMetric({ sparkline: { ...metric.sparkline, x_column: v } })}
                  />
                </div>
                <div style={{ ...styles.field, flex: 1 }}>
                  <FieldLabel helpKey="metric.sparkline.y_column">Y column</FieldLabel>
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
              <FieldLabel section helpKey="text.markdown">Text</FieldLabel>
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

          {widgetType === 'html' && (
            !htmlPanelsEnabled ? (
              <div style={styles.muted} role="note">
                HTML panels are disabled for this organization.
              </div>
            ) : (
              <>
                <FieldLabel section helpKey="html.html">Panel HTML</FieldLabel>
                <DebouncedTextInput
                  label="Panel HTML"
                  value={html.html}
                  multiline
                  placeholder={'<div class="stat">42</div>'}
                  onSave={markup => void saveWidgetConfig({ html: markup })}
                />
                <FieldLabel section helpKey="html.css">Panel CSS</FieldLabel>
                <DebouncedTextInput
                  label="Panel CSS"
                  value={html.css}
                  multiline
                  placeholder={'.stat { font-size: 28px; }'}
                  onSave={css => void saveWidgetConfig({ css })}
                />
                <div style={styles.muted}>
                  {'{{variable}}'} values are escaped as text; scripts, links, forms, and third-party
                  images are stripped at render.
                </div>
                <div style={{ ...styles.preview, height: 200, minHeight: 120 }}>
                  <HtmlWidget
                    html={html.html}
                    css={html.css}
                    theme={dashboard.settings?.app?.theme}
                    branding={dashboard.org_branding}
                  />
                </div>
              </>
            )
          )}

          {widgetType === 'custom' && (
            !customCodeEnabled ? (
              <div style={styles.muted} role="note">
                Custom code is disabled for this organization.
              </div>
            ) : (
              <>
                <FieldLabel section helpKey="custom.html">Component HTML</FieldLabel>
                <CodeMirrorField
                  label="Component HTML"
                  language="html"
                  value={custom.html}
                  editable={editingEnabled}
                  onSave={markup => void saveWidgetConfig({ html: markup })}
                />
                <FieldLabel section helpKey="custom.css">Component CSS</FieldLabel>
                <CodeMirrorField
                  label="Component CSS"
                  language="css"
                  value={custom.css}
                  editable={editingEnabled}
                  onSave={css => void saveWidgetConfig({ css })}
                />
                <FieldLabel section helpKey="custom.js">Component JS</FieldLabel>
                <CodeMirrorField
                  label="Component JS"
                  language="javascript"
                  value={custom.js}
                  editable={editingEnabled}
                  onSave={js => void saveWidgetConfig({ js })}
                />

                <FieldLabel section helpKey="custom.bindings">Data bindings</FieldLabel>
                {custom.bindings.map((binding, i) => (
                  <div key={i} style={styles.bindingRow}>
                    <input
                      aria-label="Binding key"
                      type="text"
                      style={{ ...styles.input, flex: '0 0 110px' }}
                      value={binding.key}
                      placeholder="key"
                      disabled={!editingEnabled}
                      onChange={e => saveBindings(custom.bindings.map((b, j) => (j === i ? { ...b, key: e.target.value } : b)))}
                    />
                    <select
                      aria-label="Binding widget"
                      style={{ ...styles.select, flex: 1, minWidth: 0 }}
                      value={binding.widget_id}
                      disabled={!editingEnabled}
                      onChange={e => saveBindings(custom.bindings.map((b, j) => (j === i ? { ...b, widget_id: e.target.value } : b)))}
                    >
                      {!bindableWidgets.some(w => w.id === binding.widget_id) && (
                        <option value={binding.widget_id}>(missing) {binding.widget_id.slice(0, 8)}</option>
                      )}
                      {bindableWidgets.map(w => (
                        <option key={w.id} value={w.id}>{bindingTargetLabel(w)}</option>
                      ))}
                    </select>
                    <button
                      type="button"
                      style={styles.bindingRemove}
                      title="Remove binding"
                      aria-label={`Remove binding ${binding.key}`}
                      disabled={!editingEnabled}
                      onClick={() => saveBindings(custom.bindings.filter((_, j) => j !== i))}
                    >
                      <X size={11} />
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  style={styles.bindingAdd}
                  disabled={!editingEnabled || bindableWidgets.length === 0}
                  onClick={addBinding}
                >
                  <Plus size={11} /> Add binding
                </button>
                <div style={styles.muted}>
                  Source-less: the component reads data only through these bindings.
                  Bound query widgets run as the viewer; bound cell widgets read their stored outputs.
                </div>
                <div style={styles.muted}>
                  The component runs sandboxed; it can only see data you bind here, through a bridge.
                </div>
                <div style={{ ...styles.preview, height: 240, minHeight: 120 }}>
                  <SandboxedFrame
                    html={custom.html}
                    css={custom.css}
                    js={custom.js}
                    onMessage={() => {}}
                    theme={dashboard.settings?.app?.theme}
                    branding={dashboard.org_branding}
                  />
                </div>
              </>
            )
          )}

          {isQuery && widgetType === 'table' && (
            <>
              <FieldLabel section helpKey="table.columns">Table columns</FieldLabel>
              {resultColumns.length === 0 ? (
                <div style={styles.muted}>Run the query to configure columns.</div>
              ) : (
                resultColumns.map(name => {
                  const format = tableColumns[name]
                  return (
                    <div key={name} style={styles.field}>
                      <span style={styles.fieldLabel}>{name}</span>
                      <select
                        aria-label={`Format ${name}`}
                        style={styles.select}
                        value={format?.style ?? 'default'}
                        onChange={e => saveTableColumnStyle(name, e.target.value as ValueFormat['style'] | 'default')}
                      >
                        <option value="default">Default</option>
                        <option value="number">Number</option>
                        <option value="currency">Currency</option>
                        <option value="percent">Percent</option>
                      </select>
                      {format?.style === 'currency' && (
                        <div style={styles.field}>
                          <span style={styles.fieldLabel}>Currency</span>
                          <DebouncedTextInput
                            label={`Currency for ${name}`}
                            value={format.currency ?? ''}
                            placeholder="USD"
                            onSave={v => saveTableColumn(name, { ...format, currency: normalizeCurrency(v) })}
                          />
                        </div>
                      )}
                      {format?.style !== undefined && (
                        <div style={styles.field}>
                          <span style={styles.fieldLabel}>Decimals</span>
                          <input
                            aria-label={`Decimals for ${name}`}
                            type="number"
                            min={0}
                            style={styles.input}
                            value={format.decimals ?? ''}
                            onChange={e => {
                              const raw = e.target.value
                              const parsed = raw === '' ? undefined : Number(raw)
                              const decimals = parsed !== undefined && Number.isFinite(parsed) ? Math.max(0, Math.trunc(parsed)) : undefined
                              saveTableColumn(name, { ...format, decimals })
                            }}
                          />
                        </div>
                      )}
                    </div>
                  )
                })
              )}
            </>
          )}

          <span style={styles.sectionLabel}>Appearance</span>
          <div style={styles.field}>
            <FieldLabel helpKey="appearance.background">Background</FieldLabel>
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
              id="widget-appearance-border"
              type="checkbox"
              aria-label="Border"
              checked={appearance.border}
              disabled={appearance.background === 'transparent'}
              onChange={e => void saveAppearance({ border: e.target.checked })}
            />
            <label htmlFor="widget-appearance-border">Border</label>
          </div>
          {appearance.background === 'transparent' && (
            <div style={styles.muted}>Transparent widgets never draw a border.</div>
          )}
          <div style={styles.field}>
            <FieldLabel helpKey="appearance.padding">Padding</FieldLabel>
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
