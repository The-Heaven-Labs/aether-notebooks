import { useEffect, useRef } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard, Notebook, Cell, Widget } from '../types'
import { AppShell } from '../components/AppShell'
import { OutputRenderer } from '../components/OutputRenderer'
import { DashboardParamsProvider, useDashboardParams } from '../contexts/DashboardParamsContext'

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
          type="number"
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
    borderRadius: 6,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
    background: 'white',
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

function QueryWidget({ widget }: { widget: AnyWidget }) {
  const { params } = useDashboardParams()
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const { data: notebook, isLoading } = useQuery({
    queryKey: ['notebook', widget.notebook_id],
    queryFn: () => api.get<NotebookWithCells>(`/api/v1/notebooks/${widget.notebook_id}`),
  })

  // When params change and the cell source contains template refs ({{...}}),
  // trigger a re-execution. Execute integration is a follow-on — stub here.
  useEffect(() => {
    const cell = notebook?.cells?.find((c: Cell) => c.id === widget.cell_id)
    if (!cell?.source?.includes('{{')) return

    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      // TODO: call execute API with substituted params once dashboard execute
      // endpoint is available (follow-on: POST /api/v1/dashboards/:id/execute)
    }, 300)

    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [params, notebook, widget.cell_id])

  if (isLoading) return <div style={queryWidgetStyles.loading}>Loading…</div>

  const cell = notebook?.cells?.find((c: Cell) => c.id === widget.cell_id)
  if (!cell) return <div style={queryWidgetStyles.empty}>Cell not found</div>
  if (!cell.outputs?.length) {
    return <div style={queryWidgetStyles.empty}>No results yet — run the notebook first</div>
  }
  const fixedView = widget.type === 'chart' ? 'chart' : 'table'
  return <OutputRenderer outputs={cell.outputs} fixedView={fixedView} />
}

const queryWidgetStyles: Record<string, React.CSSProperties> = {
  loading: { padding: '16px', fontSize: 13, color: 'var(--text-muted)' },
  empty: { padding: '16px', fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
}

function WidgetCard({ widget }: { widget: AnyWidget }) {
  if (INPUT_WIDGET_TYPES.has(widget.type)) {
    return (
      <div style={styles.inputWidgetCard}>
        <InputWidget widget={widget} />
      </div>
    )
  }
  return (
    <div style={styles.widgetCard}>
      <QueryWidget widget={widget} />
    </div>
  )
}

function DashboardContent({ id }: { id: string }) {
  const { data: dashboard, isLoading, error } = useQuery({
    queryKey: ['dashboard', id],
    queryFn: () => api.get<DashboardWithWidgets>(`/api/v1/dashboards/${id}`),
    enabled: !!id,
  })

  useEffect(() => {
    if (dashboard) document.title = `${dashboard.title} — Heaven's Notebooks`
    return () => { document.title = "Heaven's Notebooks" }
  }, [dashboard])

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

  const widgets = (dashboard.widgets ?? []) as AnyWidget[]
  const inputWidgets = widgets.filter((w) => INPUT_WIDGET_TYPES.has(w.type))
  const dataWidgets = widgets.filter((w) => !INPUT_WIDGET_TYPES.has(w.type))

  return (
    <AppShell>
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
          <div style={styles.empty}>
            <p style={styles.emptyText}>No widgets yet</p>
            <p style={styles.emptySubtext}>
              Add widgets in the dashboard editor to display notebook cell outputs.
            </p>
          </div>
        ) : dataWidgets.length === 0 ? null : (
          <div style={styles.grid}>
            {dataWidgets.map((widget) => (
              <WidgetCard key={widget.id} widget={widget} />
            ))}
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
    height: 52,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 24px',
    flexShrink: 0,
    position: 'sticky',
    top: -32,
    zIndex: 99,
    margin: '-32px -32px 0',
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
    color: '#6a6260',
    textDecoration: 'none',
    fontSize: 13,
    fontWeight: 500,
    flexShrink: 0,
  },
  breadcrumbSep: {
    color: '#3a3630',
    fontSize: 14,
    flexShrink: 0,
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
    maxWidth: 1280,
    margin: '0 auto',
    padding: '40px 40px',
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
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 10,
    boxShadow: 'var(--shadow-sm)',
    minWidth: 200,
    flex: '0 1 auto',
  },
  empty: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: 300,
    gap: 8,
  },
  emptyText: {
    fontSize: 16,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    margin: 0,
  },
  emptySubtext: {
    fontSize: 13,
    color: 'var(--text-muted)',
    margin: '0 0 16px',
  },
  grid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(2, 1fr)',
    gap: 16,
  },
  widgetCard: {
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 10,
    boxShadow: 'var(--shadow-sm)',
    overflow: 'hidden',
    position: 'relative',
  },
}
