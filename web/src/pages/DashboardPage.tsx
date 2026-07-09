import { useEffect, useRef, useState, useCallback, useLayoutEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft, Play, Loader2, Pencil, Settings } from 'lucide-react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard, Notebook, Cell, Widget } from '../types'
import type { ChartConfig } from '../charts/types'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { OutputRenderer } from '../components/OutputRenderer'
import { DashboardParamsProvider, useDashboardParams } from '../contexts/DashboardParamsContext'
import { GridLayout } from 'react-grid-layout'
import type { LayoutItem } from 'react-grid-layout'
import 'react-grid-layout/css/styles.css'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

interface DashboardWithWidgets extends Dashboard {
  widgets: Widget[]
}

// Widget type extended with input widget variants (config is typed loosely)
type AnyWidget = Widget & { type: string; config: any }

const INPUT_WIDGET_TYPES = new Set([
  'date_picker',
  'date_range',
  'freetext',
  'number',
  'multi_select',
])

function InputWidget({ widget }: { widget: AnyWidget }) {
  const { setParam } = useDashboardParams()
  const config = widget.config ?? {}
  const paramName: string = config.paramName ?? widget.id
  const label: string = config.label ?? paramName

  if (widget.type === 'date_picker') {
    return (
      <div style={inputStyles.wrapper}>
        <label style={inputStyles.label}>{label}</label>
        <input
          type="date"
          style={inputStyles.input}
          onChange={(e) => setParam(paramName, e.target.value)}
        />
      </div>
    )
  }

  if (widget.type === 'date_range') {
    return (
      <div style={inputStyles.wrapper}>
        <label style={inputStyles.label}>{label}</label>
        <div style={inputStyles.rangeRow}>
          <input
            type="date"
            style={{ ...inputStyles.input, flex: 1 }}
            placeholder="Start"
            onChange={(e) => setParam(`${paramName}_start`, e.target.value)}
          />
          <span style={inputStyles.rangeSep}>to</span>
          <input
            type="date"
            style={{ ...inputStyles.input, flex: 1 }}
            placeholder="End"
            onChange={(e) => setParam(`${paramName}_end`, e.target.value)}
          />
        </div>
      </div>
    )
  }

  if (widget.type === 'freetext') {
    return (
      <div style={inputStyles.wrapper}>
        <label style={inputStyles.label}>{label}</label>
        <input
          type="text"
          style={inputStyles.input}
          placeholder={config.placeholder ?? ''}
          onChange={(e) => setParam(paramName, e.target.value)}
        />
      </div>
    )
  }

  if (widget.type === 'number') {
    return (
      <div style={inputStyles.wrapper}>
        <label style={inputStyles.label}>{label}</label>
        <input
          type="text"
          style={inputStyles.input}
          placeholder={config.placeholder ?? ''}
          onChange={(e) => setParam(paramName, e.target.value)}
        />
      </div>
    )
  }

  if (widget.type === 'multi_select') {
    const options: string[] = Array.isArray(config.options) ? config.options : []
    return (
      <div style={inputStyles.wrapper}>
        <label style={inputStyles.label}>{label}</label>
        <select
          multiple
          style={{ ...inputStyles.input, height: 'auto', minHeight: 80 }}
          onChange={(e) => {
            const selected = Array.from(e.target.selectedOptions).map((o) => o.value)
            setParam(paramName, selected.join(','))
          }}
        >
          {options.map((opt) => (
            <option key={opt} value={opt}>{opt}</option>
          ))}
        </select>
        <span style={inputStyles.hint}>Hold Ctrl / Cmd to select multiple</span>
      </div>
    )
  }

  return null
}

const inputStyles: Record<string, React.CSSProperties> = {
  wrapper: {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    padding: '14px 16px',
  },
  label: {
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
  },
  input: {
    padding: '7px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    width: '100%',
    boxSizing: 'border-box',
    outline: 'none',
  },
  rangeRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  rangeSep: {
    fontSize: 12,
    color: 'var(--text-muted)',
    flexShrink: 0,
  },
  hint: {
    fontSize: 11,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
}

function QueryWidget({ widget, qc, widgetsData, dashboardId }: { widget: AnyWidget; qc: ReturnType<typeof useQueryClient>; widgetsData?: DashboardWithWidgets['widgets_data']; dashboardId?: string }) {
  const { params } = useDashboardParams()
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Use widgets_data when available (view_with_data permission) instead of fetching notebooks individually
  const widgetCellData = widgetsData?.[widget.cell_id!]
  const useInlinedData = !!widgetCellData

  const { data: notebook, isLoading } = useQuery({
    queryKey: ['notebook', widget.notebook_id],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${widget.notebook_id}`),
    enabled: !useInlinedData,
  })

  // When params change and the cell source contains template refs ({{...}}),
  // trigger a re-execution. Execute integration is a follow-on — stub here.
  useEffect(() => {
    const cell = useInlinedData
      ? widgetCellData
      : notebook?.cells?.find((c: Cell) => c.id === widget.cell_id)
    if (!cell?.source?.includes('{{')) return

    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      // TODO: call execute API with substituted params once dashboard execute
      // endpoint is available (follow-on: POST /api/v1/dashboards/:id/execute)
    }, 300)

    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [params, notebook, widget.cell_id, widgetCellData, useInlinedData])

  if (!useInlinedData && isLoading) return <div style={queryWidgetStyles.loading}>Loading…</div>

  const cell = useInlinedData
    ? widgetCellData!
    : notebook?.cells?.find((c: Cell) => c.id === widget.cell_id)
  if (!cell) return <div style={queryWidgetStyles.empty}>Cell not found</div>

  // Markdown cells render their source directly — they don't need to be "run"
  if (cell.type === 'text') {
    return (
      <div style={queryWidgetStyles.markdown}>
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{cell.source || ''}</ReactMarkdown>
      </div>
    )
  }

  if (!cell.outputs?.length) {
    return (
      <div style={queryWidgetStyles.empty}>
        No data yet.{' '}
        <button
          style={{ color: 'var(--accent)', background: 'none', border: 'none', cursor: 'pointer', fontSize: 13, padding: 0 }}
          onClick={() => {
            const token = localStorage.getItem('aether_token')
            fetch(`/api/v1/notebooks/${widget.notebook_id}/cells/${widget.cell_id}/execute`, {
              method: 'POST',
              headers: {
                'Content-Type': 'application/json',
                ...(token ? { Authorization: `Bearer ${token}` } : {}),
              },
              body: JSON.stringify({ parameters: {} }),
            }).then(() => {
              if (useInlinedData && dashboardId) {
                qc.invalidateQueries({ queryKey: ['dashboard', dashboardId] })
              } else {
                qc.invalidateQueries({ queryKey: ['notebook', widget.notebook_id] })
              }
            })
          }}
        >
          Run cell
        </button>
      </div>
    )
  }
  const fixedView = widget.type === 'chart' ? 'chart' : 'table'
  const chartConfig = ((cell as any).metadata?.chart ?? widget.config) as ChartConfig | undefined
  const updatedAt = (cell as any).updated_at
  const durationMs = (cell as any).duration_ms
  return (
    <>
      <OutputRenderer outputs={cell.outputs} fixedView={fixedView} chartConfig={chartConfig} />
      {updatedAt && (
        <div style={queryWidgetStyles.footer}>
          Executed at {new Date(updatedAt).toLocaleDateString([], { year: 'numeric', month: '2-digit', day: '2-digit' })} {new Date(updatedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
          {durationMs != null && <span> · {durationMs}ms</span>}
        </div>
      )}
    </>
  )
}

const queryWidgetStyles: Record<string, React.CSSProperties> = {
  loading: { padding: '16px', fontSize: 13, color: 'var(--text-muted)' },
  empty: { padding: '16px', fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  markdown: { padding: '16px', fontSize: 14, color: 'var(--text-primary)', lineHeight: 1.6, overflow: 'auto', height: '100%' },
  footer: { fontSize: 10, color: 'var(--text-muted)', padding: '4px 12px', borderTop: '1px solid var(--border-light)', opacity: 0.6 },
}

function WidgetCard({ widget, qc, widgetsData, dashboardId, onEdit }: { widget: AnyWidget; qc: ReturnType<typeof useQueryClient>; widgetsData?: DashboardWithWidgets['widgets_data']; dashboardId?: string; onEdit?: () => void }) {
  const [loading, setLoading] = useState(false)

  const handleRun = useCallback(async () => {
    if (loading || !widget.notebook_id || !widget.cell_id) return
    setLoading(true)
    try {
      const token = localStorage.getItem('aether_token')
      await fetch(`/api/v1/notebooks/${widget.notebook_id}/cells/${widget.cell_id}/execute`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({ parameters: {} }),
      })
      qc.invalidateQueries({ queryKey: ['notebook', widget.notebook_id] })
    } finally {
      setLoading(false)
    }
  }, [widget.notebook_id, widget.cell_id, qc, loading])

  if (INPUT_WIDGET_TYPES.has(widget.type)) {
    return (
      <div style={styles.inputWidgetCard}>
        <InputWidget widget={widget} />
      </div>
    )
  }
  return (
    <div style={styles.widgetCard}>
      <div style={styles.widgetPlayBar}>
        <button
          style={styles.widgetPlayBtn}
          onClick={handleRun}
          disabled={loading}
          title="Refresh widget data"
        >
          {loading
            ? <Loader2 size={12} style={{ animation: 'spin 1s linear infinite' }} />
            : <Play size={12} />}
        </button>
        {onEdit && (
          <button
            style={styles.widgetEditBtn}
            onClick={onEdit}
            title="Edit widget"
          >
            <Pencil size={11} />
          </button>
        )}
      </div>
      <QueryWidget widget={widget} qc={qc} widgetsData={widgetsData} dashboardId={dashboardId} />
    </div>
  )
}

const toGridItem = (w: Widget): LayoutItem => ({
  i: w.id,
  x: w.layout.col,
  y: w.layout.row,
  w: w.layout.width,
  h: w.layout.height,
})

function DashboardContent({ id }: { id: string }) {
  const qc = useQueryClient()
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [containerWidth, setContainerWidth] = useState(0)
  const [refreshSeconds, setRefreshSeconds] = useState<number>(0)
  const [refreshCustom, setRefreshCustom] = useState(false)
  const gridContainerRef = useRef<HTMLDivElement | null>(null)

  const gridRef = useCallback((el: HTMLDivElement | null) => {
    gridContainerRef.current = el
    if (el) {
      setContainerWidth(el.clientWidth)
    }
  }, [])

  useEffect(() => {
    const el = gridContainerRef.current
    if (!el) return
    const obs = new ResizeObserver(([entry]) => {
      setContainerWidth(entry.contentRect.width)
    })
    obs.observe(el)
    return () => obs.disconnect()
  }, [])

  useLayoutEffect(() => {
    const el = gridContainerRef.current
    if (!el) return
    const w = el.clientWidth
    if (w !== containerWidth) {
      setContainerWidth(w)
    }
  })

  const { data: dashboard, isLoading, error } = useQuery({
    queryKey: ['dashboard', id],
    queryFn: () => api.get<DashboardWithWidgets>(`/api/v1/dashboards/${id}`),
    enabled: !!id,
  })

  useEffect(() => {
    if (dashboard) {
      document.title = `${dashboard.title} — Aether Notebooks`
      const el = gridContainerRef.current
      if (el) setContainerWidth(el.clientWidth)
    }
    return () => { document.title = "Aether Notebooks" }
  }, [dashboard])

  async function executeAllWidgets(widgetList: AnyWidget[]) {
    const token = localStorage.getItem('aether_token')
    const queryWidgets = widgetList.filter(w => !INPUT_WIDGET_TYPES.has(w.type))
    if (!queryWidgets.length) return
    setIsRefreshing(true)
    try {
      await Promise.all(
        queryWidgets
          .filter(w => w.notebook_id && w.cell_id)
          .map(w =>
            fetch(`/api/v1/notebooks/${w.notebook_id}/cells/${w.cell_id}/execute`, {
              method: 'POST',
              headers: {
                'Content-Type': 'application/json',
                ...(token ? { Authorization: `Bearer ${token}` } : {}),
              },
              body: JSON.stringify({ parameters: {} }),
            })
          )
      )
      if (dashboard?.can_view_with_data) {
        qc.invalidateQueries({ queryKey: ['dashboard', id] })
      } else {
        const notebookIds = [...new Set(queryWidgets.map(w => w.notebook_id).filter(Boolean))]
        notebookIds.forEach(nbId => qc.invalidateQueries({ queryKey: ['notebook', nbId] }))
      }
    } finally {
      setIsRefreshing(false)
    }
  }

  const widgets = (dashboard?.widgets ?? []) as AnyWidget[]
  const autoRefreshSecs = dashboard?.settings?.auto_refresh_seconds ?? 0
  const PRESET_REFRESH = [0, 30, 60, 300, 600]
  const customRefreshText = refreshCustom && refreshSeconds > 0 ? ` — ${refreshSeconds}s` : ''

  // Sync local state from dashboard data
  useEffect(() => {
    setRefreshSeconds(autoRefreshSecs)
    setRefreshCustom(!PRESET_REFRESH.includes(autoRefreshSecs))
  }, [autoRefreshSecs])

  useEffect(() => {
    if (!refreshSeconds || refreshSeconds <= 0 || !widgets.length) return
    const intervalId = setInterval(() => executeAllWidgets(widgets), refreshSeconds * 1000)
    return () => clearInterval(intervalId)
  }, [refreshSeconds, widgets])

  if (isLoading) {
    return (
      <div style={styles.loadingPage}>
        <div style={styles.loadingDot} />
      </div>
    )
  }

  if (error || !dashboard) {
    return (
      <div style={styles.loadingPage}>
        <p style={{ color: 'var(--text-secondary)' }}>
          {error ? (error as Error).message : 'Dashboard not found'}
        </p>
      </div>
    )
  }

  const inputWidgets = widgets.filter((w) => INPUT_WIDGET_TYPES.has(w.type))
  const dataWidgets = widgets.filter((w) => !INPUT_WIDGET_TYPES.has(w.type))

  return (
    <AppShell noPadding>
      {/* Sub-header */}
      <header style={styles.subHeader}>
        <div style={styles.headerLeft}>
          <Link to="/dashboards" style={styles.backLink}>
            <ArrowLeft size={14} style={{ flexShrink: 0 }} />
            <span>Dashboards</span>
          </Link>
          <span style={styles.breadcrumbSep}>/</span>
          <span style={styles.dashboardTitle}>{dashboard.title}</span>
        </div>
        {/* Run all + auto-refresh */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <button
            style={{
              padding: '5px 12px', fontSize: 12, fontWeight: 600,
              background: 'var(--accent)', color: '#fff',
              border: 'none', borderRadius: 4, cursor: 'pointer',
              opacity: isRefreshing ? 0.6 : 1,
            }}
            disabled={isRefreshing}
            onClick={() => executeAllWidgets(widgets)}
            title="Execute all widget cells"
          >
            {isRefreshing ? 'Running…' : 'Run all'}
          </button>
          <Link
            to={`/dashboards/${id}`}
            style={{
              padding: '5px 12px', fontSize: 12, fontWeight: 600,
              background: 'none', color: 'var(--text-secondary)',
              border: '1px solid var(--border)', borderRadius: 4,
              textDecoration: 'none', cursor: 'pointer',
              display: 'flex', alignItems: 'center', gap: 4,
            }}
            title="Edit dashboard layout"
          >
            <Settings size={12} /> Edit
          </Link>

          {/* Column count selector */}
          <div style={{ display: 'flex', gap: 2, background: 'var(--border-light)', padding: 2, borderRadius: 4 }}>
            {[6, 8, 12, 16, 24].map(cols => (
              <button
                key={cols}
                style={{
                  padding: '3px 8px', fontSize: 11, fontWeight: 500,
                  border: '1px solid transparent', borderRadius: 4,
                  background: (dashboard?.settings?.grid_cols ?? 12) === cols ? 'var(--bg-card)' : 'none',
                  color: (dashboard?.settings?.grid_cols ?? 12) === cols ? 'var(--text-primary)' : 'var(--text-secondary)',
                  cursor: 'pointer',
                }}
                onClick={async () => {
                  await api.put(`/api/v1/dashboards/${id}`, {
                    settings: { ...dashboard?.settings, grid_cols: cols },
                  })
                  qc.invalidateQueries({ queryKey: ['dashboard', id] })
                }}
                title={`${cols} columns`}
              >
                {cols}
              </button>
            ))}
          </div>

          <div style={{ position: 'relative' }}>
            <select
              style={{
                fontSize: 12, padding: '4px 24px 4px 8px', border: '1px solid var(--border)',
                borderRadius: 4, background: 'var(--bg-input)', color: 'var(--text-secondary)',
                cursor: 'pointer', appearance: 'none', WebkitAppearance: 'none', MozAppearance: 'none',
              }}
              value={refreshCustom ? 'custom' : String(refreshSeconds)}
              onChange={async e => {
                const val = e.target.value
                if (val === 'custom') { setRefreshCustom(true); return }
                setRefreshCustom(false)
                const secs = parseInt(val)
                setRefreshSeconds(secs)
                if (secs === autoRefreshSecs) return
                await api.put(`/api/v1/dashboards/${id}`, {
                  settings: { ...dashboard?.settings, auto_refresh_seconds: secs },
                })
                qc.invalidateQueries({ queryKey: ['dashboard', id] })
              }}
              title="Auto-refresh interval"
              aria-label="Auto-refresh interval"
            >
              <option value="0">No auto-refresh</option>
              <option value="30">Every 30s</option>
              <option value="60">Every 1m</option>
              <option value="300">Every 5m</option>
              <option value="600">Every 10m</option>
              <option value="custom">Custom{customRefreshText}</option>
            </select>
            <svg viewBox="0 0 10 6" width="10" height="6" style={{ position: 'absolute', right: 8, top: '50%', transform: 'translateY(-50%)', pointerEvents: 'none', fill: 'none', stroke: 'currentColor', strokeWidth: 1.5 }}>
              <polyline points="1,1 5,5 9,1" />
            </svg>
          </div>
          {refreshCustom && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginLeft: 4 }}>
              <input
                type="text"
               
               
                style={{
                  width: 72, fontSize: 12, padding: '4px 6px', border: '1px solid var(--border)',
                  borderRadius: 4, background: 'var(--bg-input)', color: 'var(--text-secondary)',
                }}
                value={refreshSeconds}
                onChange={e => {
                  const val = parseInt(e.target.value)
                  if (!isNaN(val) && val >= 0) setRefreshSeconds(val)
                }}
                onBlur={async () => {
                  if (refreshSeconds === autoRefreshSecs) return
                  await api.put(`/api/v1/dashboards/${id}`, {
                    settings: { ...dashboard?.settings, auto_refresh_seconds: refreshSeconds },
                  })
                  qc.invalidateQueries({ queryKey: ['dashboard', id] })
                }}
                onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur() }}
                title="Custom refresh interval in seconds (0 = off)"
                aria-label="Custom refresh interval in seconds"
              />
              <span style={{ fontSize: 11, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>s</span>
            </div>
          )}
        </div>
      </header>

      <div style={styles.body}>
        {/* Input widgets row — rendered at the top when present */}
        {inputWidgets.length > 0 && (
          <div style={styles.inputRow}>
            {inputWidgets.map((widget) => (
              <div key={widget.id} style={styles.inputWidgetCard}>
                <InputWidget widget={widget} />
              </div>
            ))}
          </div>
        )}

        {/* Data widgets grid */}
        {dataWidgets.length === 0 && inputWidgets.length === 0 ? (
          <EmptyState
            title="No widgets yet"
            text="Add widgets in the dashboard editor to display notebook cell outputs."
          />
        ) : dataWidgets.length === 0 ? null : (
          <div ref={gridRef}>
            <GridLayout
              layout={dataWidgets.map(toGridItem)}
              width={containerWidth}
              gridConfig={{ cols: dashboard.settings?.grid_cols ?? 12, rowHeight: 30, margin: [4, 4] }}
              dragConfig={{ enabled: false }}
              resizeConfig={{ enabled: false }}
              style={{ minHeight: 240 }}
            >
              {dataWidgets.map((widget) => (
                <div key={widget.id} style={styles.widgetCard}>
                  <WidgetCard widget={widget} qc={qc} widgetsData={dashboard.widgets_data} dashboardId={id} />
                </div>
              ))}
            </GridLayout>
          </div>
        )}
      </div>
    </AppShell>
  )
}

export function DashboardPage() {
  const { id } = useParams<{ id: string }>()
  return (
    <DashboardParamsProvider>
      <DashboardContent id={id ?? ''} />
    </DashboardParamsProvider>
  )
}

const styles: Record<string, React.CSSProperties> = {
  loadingPage: {
    minHeight: '100vh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  loadingDot: {
    width: 8,
    height: 8,
    borderRadius: '50%',
    background: 'var(--accent)',
    opacity: 0.5,
  },
  subHeader: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    height: 44,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 12px',
    flexShrink: 0,
    position: 'sticky',
    top: 0,
    zIndex: 99,
  },
  headerLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    minWidth: 0,
  },
  backLink: {
    display: 'flex',
    alignItems: 'center',
    gap: 5,
    color: 'var(--nav-text)',
    textDecoration: 'none',
    fontSize: 13,
    fontWeight: 500,
    flexShrink: 0,
    opacity: 0.8,
  },
  breadcrumbSep: {
    color: 'var(--nav-text)',
    fontSize: 14,
    flexShrink: 0,
    opacity: 0.5,
  },
  dashboardTitle: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--nav-text)',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    maxWidth: 400,
  },
  body: {
    flex: 1,
    margin: 0,
    padding: '8px 12px',
    width: '100%',
    display: 'flex',
    flexDirection: 'column',
    gap: 24,
  },
  inputRow: {
    display: 'flex',
    flexWrap: 'wrap',
    gap: 12,
  },
  inputWidgetCard: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    minWidth: 200,
    flex: '0 1 auto',
  },

  widgetCard: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border-light)',
    borderRadius: 4,
    overflow: 'hidden',
    height: '100%',
  },
  widgetPlayBar: {
    position: 'absolute',
    top: 6,
    right: 6,
    display: 'flex',
    gap: 4,
    zIndex: 2,
  },
  widgetPlayBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 24,
    height: 24,
    borderRadius: 4,
    border: '1px solid var(--border)',
    background: 'var(--bg-card)',
    color: 'var(--text-muted)',
    cursor: 'pointer',
    padding: 0,
  },
  widgetEditBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 24,
    height: 24,
    borderRadius: 4,
    border: '1px solid var(--border)',
    background: 'var(--bg-card)',
    color: 'var(--text-muted)',
    cursor: 'pointer',
    padding: 0,
  },
}
