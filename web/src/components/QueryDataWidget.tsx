import { useCallback, useEffect, useRef, useState } from 'react'
import { Loader2, RefreshCw, FilterX, BarChart3 } from 'lucide-react'
import { ApiError } from '../api/client'
import { useDashboardVariables, isVariableDefault } from '../contexts/DashboardVariablesContext'
import { useWidgetQuery } from '../hooks/useWidgetQuery'
import { OutputRenderer } from './OutputRenderer'
import { normalizeChartConfig } from '../charts/normalizeChartConfig'
import { formatExecutedAt } from '../utils/formatDateTime'
import type { DashboardVariable, ResultSet, Widget } from '../types'

const styles: Record<string, React.CSSProperties> = {
  root: {
    flex: 1,
    minHeight: 0,
    display: 'flex',
    flexDirection: 'column',
    transition: 'opacity 0.15s ease',
  },
  muted: { padding: '16px', fontSize: 13, color: 'var(--text-muted)' },
  empty: { padding: '16px', fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  error: { padding: '16px', fontSize: 13, color: 'var(--danger, #d33)', display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' },
  retry: {
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontSize: 12,
    padding: '3px 10px',
  },
  footer: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    flex: 1,
    minWidth: 0,
  },
  footerBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 20,
    height: 20,
    borderRadius: 3,
    border: '1px solid var(--border)',
    background: 'var(--bg-card)',
    color: 'var(--text-muted)',
    cursor: 'pointer',
    padding: 0,
  },
  footerText: {
    marginLeft: 'auto',
    fontSize: 11,
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap',
    minWidth: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  // Compact version of the product's empty-state pattern for widget-sized space.
  zero: {
    flex: 1,
    minHeight: 0,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    padding: 16,
    textAlign: 'center',
  },
  zeroIcon: {
    width: 36,
    height: 36,
    marginBottom: 2,
    background: 'var(--accent-light)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: 'var(--text-muted)',
  },
  zeroTitle: { margin: 0, fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' },
  zeroText: { margin: 0, fontSize: 12, color: 'var(--text-secondary)', maxWidth: 380 },
  zeroChips: { display: 'flex', flexWrap: 'wrap', gap: 4, justifyContent: 'center', maxWidth: '90%' },
  zeroChip: {
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    padding: '1px 5px',
    borderRadius: 3,
    border: '1px solid var(--border)',
    color: 'var(--text-secondary)',
    background: 'var(--bg-input)',
    maxWidth: 200,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  zeroAction: {
    marginTop: 4,
    padding: '5px 12px',
    fontSize: 12,
    fontWeight: 600,
    background: 'none',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
  },
}

function formatFilterValue(v: DashboardVariable, value: unknown): string {
  if (v.type === 'multi_select') {
    const list = Array.isArray(value) ? value : []
    return list.length ? `${list.length} selected` : 'none'
  }
  if (v.type === 'date_range') {
    const pair = Array.isArray(value) ? value : []
    if (pair.length === 2) return `${pair[0]} → ${pair[1]}`
    return 'not set'
  }
  if (v.type === 'boolean') return value === true ? 'on' : 'off'
  if (value === undefined || value === null || value === '') return 'not set'
  return String(value)
}

function FilteredZeroState({ active, values, emptyMulti, onReset }: {
  active: DashboardVariable[]
  values: Record<string, unknown>
  emptyMulti: DashboardVariable[]
  onReset: () => void
}) {
  const shown = active.slice(0, 4)
  const extra = active.length - shown.length
  const emptyLabels = emptyMulti.map(v => v.label || v.name).join(', ')
  return (
    <div style={styles.zero} role="status">
      <div style={styles.zeroIcon}><FilterX size={16} /></div>
      <p style={styles.zeroTitle}>No rows match the current filters</p>
      <p style={styles.zeroText}>
        {emptyMulti.length > 0
          ? `Nothing is selected for ${emptyLabels} — an empty selection excludes every row.`
          : 'The dashboard variables are hiding all rows. Reset them to see data again.'}
      </p>
      {shown.length > 0 && (
        <div style={styles.zeroChips}>
          {shown.map(v => (
            <span key={v.name} style={styles.zeroChip} title={`${v.label || v.name} = ${formatFilterValue(v, values[v.name])}`}>
              {v.name}={formatFilterValue(v, values[v.name])}
            </span>
          ))}
          {extra > 0 && <span style={styles.zeroChip}>+{extra} more</span>}
        </div>
      )}
      <button type="button" style={styles.zeroAction} onClick={onReset}>Reset filters</button>
    </div>
  )
}

function NoDataState({ onRun }: { onRun: () => void }) {
  return (
    <div style={styles.zero} role="status">
      <div style={styles.zeroIcon}><BarChart3 size={16} /></div>
      <p style={styles.zeroTitle}>No data returned</p>
      <p style={styles.zeroText}>This widget's query returned no rows.</p>
      <button type="button" style={styles.zeroAction} onClick={onRun}>Run again</button>
    </div>
  )
}

export function QueryDataWidget({ dashboardId, widget, canViewWithData, queryEnabled = true, viewerConnectorId, endpointBase, onFetchingChange, registerRefresher }: {
  dashboardId: string
  widget: Widget
  canViewWithData: boolean
  /** Gates the query itself (e.g. while the viewer's connector default resolves). */
  queryEnabled?: boolean
  /** Viewer's dashboard connector selection; unset for public dashboards. */
  viewerConnectorId?: string | null
  /** Overrides the execute endpoint base, e.g. `/api/v1/public/{token}`. */
  endpointBase?: string
  /** Reports this widget's fetching state so the dashboard header can show progress. */
  onFetchingChange?: (fetching: boolean) => void
  /** Registers an awaitable refresher; return value unregisters on unmount. */
  registerRefresher?: (refresh: () => Promise<unknown>) => (() => void) | void
}) {
  const { variables, values, resetAll } = useDashboardVariables()
  const { data, error, isPending, isFetching, refresh } = useWidgetQuery({
    dashboardId,
    widget,
    values,
    enabled: canViewWithData && queryEnabled,
    viewerConnectorId,
    endpointBase,
  })
  const [fetchedAt, setFetchedAt] = useState<Date | null>(null)

  useEffect(() => {
    if (data) setFetchedAt(new Date())
  }, [data])

  // Stable refresher handle: the dashboard header can await "Refresh" runs.
  const refreshRef = useRef(refresh)
  refreshRef.current = refresh
  const stableRefresh = useCallback(() => refreshRef.current(), [])
  useEffect(() => registerRefresher?.(stableRefresh), [registerRefresher, stableRefresh])

  // Report fetching state upward (header progress). Effects run the ref so
  // unstable parent callbacks never retrigger the report.
  const onFetchingChangeRef = useRef(onFetchingChange)
  onFetchingChangeRef.current = onFetchingChange
  useEffect(() => {
    onFetchingChangeRef.current?.(isFetching)
  }, [isFetching])
  useEffect(() => () => onFetchingChangeRef.current?.(false), [])

  if (!canViewWithData) {
    return <div style={styles.muted}>You need view_with_data access to see data for this widget.</div>
  }

  if (isPending && !error) {
    return <div style={styles.muted}>Loading…</div>
  }

  if (error) {
    const forbidden = error instanceof ApiError && error.status === 403
    return (
      <div style={styles.error}>
        <span>{error instanceof Error ? error.message : 'Query failed'}</span>
        {!forbidden && (
          <button style={styles.retry} onClick={() => refreshRef.current()}>Retry</button>
        )}
      </div>
    )
  }

  const outputs = data?.outputs ?? []
  const resultSet = outputs[0]?.data as ResultSet | undefined
  const rowCount = resultSet?.rows?.length ?? 0

  // "Zero rows" is only ever rendered as a zero: when the filters can be
  // blamed (empty multi-selects, or active variables plus no rows) the widget
  // says so and offers a way back instead of dressing the exclusion up as data.
  // count()-style widgets always return one row, so the empty-selection case
  // must win over the row count.
  const emptyMulti = variables.filter(
    v => v.type === 'multi_select' && Array.isArray(values[v.name]) && (values[v.name] as unknown[]).length === 0,
  )
  const activeFilters = variables.filter(v => !isVariableDefault(v, values[v.name]))
  const filtersExclude = emptyMulti.length > 0 || (rowCount === 0 && activeFilters.length > 0)
  const noRows = !outputs.length || rowCount === 0

  if (filtersExclude || noRows) {
    return (
      <div style={{ ...styles.root, opacity: isFetching ? 0.55 : 1 }} aria-busy={isFetching}>
        {filtersExclude ? (
          <FilteredZeroState active={activeFilters} values={values} emptyMulti={emptyMulti} onReset={resetAll} />
        ) : (
          <NoDataState onRun={() => refreshRef.current()} />
        )}
      </div>
    )
  }

  const widgetTitle = (() => {
    const cfg = widget.config as Record<string, unknown> | undefined
    const t = typeof cfg?.title === 'string' ? cfg.title : typeof cfg?.label === 'string' ? cfg.label : ''
    return t || null
  })()

  const footerExtra = (
    <div style={styles.footer}>
      <button
        style={styles.footerBtn}
        onClick={() => refreshRef.current()}
        disabled={isFetching}
        title={widgetTitle ? `Refresh ${widgetTitle}` : 'Refresh widget data'}
        aria-label={widgetTitle ? `Refresh ${widgetTitle}` : 'Refresh widget data'}
      >
        {isFetching ? <Loader2 size={10} style={{ animation: 'spin 1s linear infinite' }} /> : <RefreshCw size={10} />}
      </button>
      {fetchedAt && (
        <span style={styles.footerText}>
          {/* Same date + time format as the cell widgets' "Executed at". */}
          Executed at {formatExecutedAt(fetchedAt)}
          {data?.cached && <span> · cached</span>}
          {data?.metrics?.query_time_ms != null && <span> · {data.metrics.query_time_ms}ms</span>}
        </span>
      )}
      {data?.cached && <RefreshCw size={10} style={{ color: 'var(--text-muted)' }} />}
    </div>
  )

  return (
    <div style={{ ...styles.root, opacity: isFetching ? 0.55 : 1 }} aria-busy={isFetching}>
      <OutputRenderer
        outputs={outputs}
        fixedView={widget.type === 'chart' ? 'chart' : 'table'}
        chartConfig={normalizeChartConfig(widget.config)}
        footerExtra={footerExtra}
      />
    </div>
  )
}
