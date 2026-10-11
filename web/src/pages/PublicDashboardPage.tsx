import { useRef, useState, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api } from '../api/client'
import { Skeleton } from '../components/Skeleton'
import { OutputRenderer } from '../components/OutputRenderer'
import { DashboardVariablesProvider } from '../contexts/DashboardVariablesContext'
import { DashboardVariableBar } from '../components/DashboardVariableBar'
import { QueryDataWidget } from '../components/QueryDataWidget'
import { TextWidget } from '../components/TextWidget'
import { APP_CONTENT_MAX_WIDTH } from '../components/AppHeader'
import { formatExecutedAt } from '../utils/formatDateTime'
import { textConfigFrom } from '../utils/textConfig'
import { isQueryWidget, isTextPanel } from '../utils/widgetKind'
import type { Dashboard, Widget, Output } from '../types'
import { mergeWidgetChartConfig } from '../charts/widgetChartConfig'

interface DashboardWithWidgets extends Dashboard {
  widgets: Widget[]
  widgets_data?: Record<string, { cell_id: string; source: string; type: string; language: string; outputs: Output[]; metadata?: Record<string, unknown>; updated_at?: string }>
}

const ROW_HEIGHT = 30
const MARGIN = 4

export function PublicDashboardPage() {
  const { token } = useParams<{ token: string }>()
  const { data: dashboard, isLoading, error } = useQuery({
    queryKey: ['public', token],
    queryFn: () => api.get<DashboardWithWidgets>(`/api/v1/public/${token}`),
    enabled: !!token,
  })

  const gridRef = useRef<HTMLDivElement | null>(null)
  const [containerWidth, setContainerWidth] = useState(800)
  const [gap] = useState(MARGIN)

  // Observe the grid as soon as it mounts (a mount-time effect would run
  // while the loading skeleton is up and never attach), so window resizes
  // reflow the layout without a reload.
  const gridObserverRef = useRef<ResizeObserver | null>(null)
  const measureRef = useCallback((el: HTMLDivElement | null) => {
    gridRef.current = el
    gridObserverRef.current?.disconnect()
    gridObserverRef.current = null
    if (el) {
      setContainerWidth(el.clientWidth)
      const ro = new ResizeObserver((entries) => {
        for (const entry of entries) {
          setContainerWidth(entry.contentRect.width)
        }
      })
      ro.observe(el)
      gridObserverRef.current = ro
    }
  }, [])

  if (isLoading) return <div style={{ padding: 40 }}><Skeleton count={5} height={40} /></div>
  if (error) return <div style={{ padding: 40, color: 'var(--error)' }}>Not found or sharing disabled</div>
  if (!dashboard) return null

  const widgets = dashboard.widgets ?? []
  const widgetsData = dashboard.widgets_data ?? {}
  const publicLive = dashboard.settings?.public_live === true
  const variables = dashboard.settings?.variables ?? []
  const app = dashboard.settings?.app
  const appWidth = app?.width ?? 'fixed'
  const appTitle = app?.title || dashboard.title
  // Masthead and body share one content width (The Story).
  const contentWidth: React.CSSProperties | undefined = appWidth === 'fixed'
    ? { maxWidth: APP_CONTENT_MAX_WIDTH, margin: '0 auto' }
    : undefined
  // A non-live page doesn't mount the variables provider (its option loads are
  // live-only and would 403), so text panels interpolate against the
  // variables' configured defaults instead of an empty context.
  const variableDefaults: Record<string, unknown> = {}
  for (const v of variables) {
    if (v.default !== undefined) variableDefaults[v.name] = v.default
  }
  const endpointBase = `/api/v1/public/${token}`
  // Query widgets only render when the owner opted into live queries; cell
  // widgets need stored outputs to render at all; source-less text panels
  // always render — except empty ones, which have nothing to show (the
  // editor's "empty text panel" hint is wrong on a public surface).
  const visibleWidgets = widgets.filter((w) => {
    if (isQueryWidget(w)) return publicLive
    if (isTextPanel(w)) return textConfigFrom(w.config).markdown.trim().length > 0
    return !!(w.cell_id && widgetsData[w.cell_id]?.outputs?.length)
  })
  const cols = dashboard.settings?.grid_cols ?? 12
  const colWidth = (containerWidth - (cols - 1) * gap) / cols
  // Widgets are never shorter than 6 rows here either: below that the chart
  // area collapses under the widget chrome (~40px plots).
  const totalHeight = visibleWidgets.reduce((max, w) => {
    return Math.max(max, (w.layout?.row ?? 0) + Math.max(w.layout?.height ?? 4, 6))
  }, 0) * (ROW_HEIGHT + gap)

  const content = (
    <div style={pageStyles.page}>
      <header style={pageStyles.header}>
        <div style={{ ...pageStyles.headerInner, ...contentWidth }}>
          <span style={pageStyles.brandMark}>Aether</span>
          <div style={pageStyles.titleBlock}>
            <h1 style={pageStyles.title}>{appTitle}</h1>
            {app?.subtitle && <div style={pageStyles.subtitle}>{app.subtitle}</div>}
          </div>
          <span style={pageStyles.readOnlyBadge}>{publicLive ? 'Live' : 'Read-only'}</span>
        </div>
      </header>
      <main style={{ ...pageStyles.body, ...contentWidth }}>
        {publicLive && variables.length > 0 && (
          <div style={{ marginBottom: 20 }}>
            <DashboardVariableBar />
          </div>
        )}
        {visibleWidgets.length === 0 ? (
          <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
            <p style={{ fontSize: 14, fontWeight: 600, margin: '0 0 4px' }}>No widgets in this dashboard</p>
            <p style={{ fontSize: 13, margin: 0 }}>The dashboard owner hasn't added any widgets yet.</p>
          </div>
        ) : (
          <div ref={measureRef} style={{ minHeight: totalHeight, position: 'relative' }}>
            {visibleWidgets.map((widget) => {
              const l = widget.layout
              const left = l.col * (colWidth + gap)
              const top = l.row * (ROW_HEIGHT + gap)
              const width = l.width * colWidth + (l.width - 1) * gap
              const widgetRows = Math.max(l.height, 6)
              const height = widgetRows * ROW_HEIGHT + (widgetRows - 1) * gap
              const wrapperStyle: React.CSSProperties = {
                position: 'absolute',
                left, top, width, height,
                background: 'var(--bg-card)',
                border: '1px solid var(--border)',
                borderRadius: 4,
                overflow: 'hidden',
                display: 'flex',
                flexDirection: 'column',
              }

              if (isQueryWidget(widget)) {
                return (
                  <div key={widget.id} style={wrapperStyle}>
                    <QueryDataWidget
                      dashboardId={dashboard.id}
                      widget={widget}
                      canViewWithData
                      endpointBase={endpointBase}
                    />
                  </div>
                )
              }

              if (isTextPanel(widget)) {
                return (
                  <div key={widget.id} style={wrapperStyle}>
                    {/* Live pages read the provider's current values (URL /
                        stored / defaults); read-only pages interpolate the
                        configured defaults so `{{tokens}}` never render raw. */}
                    <TextWidget
                      markdown={textConfigFrom(widget.config).markdown}
                      values={publicLive ? undefined : variableDefaults}
                      variables={variables}
                    />
                  </div>
                )
              }

              const cellData = widgetsData[widget.cell_id!]
              const isChart = widget.type === 'chart'
              const fixedView = isChart ? 'chart' : 'table'
              const chartConfig = mergeWidgetChartConfig(cellData.metadata?.chart, widget.config)

              return (
                <div key={widget.id} style={wrapperStyle}>
                  {cellData.type === 'text' ? (
                    <div style={{ padding: 16, fontSize: 14, color: 'var(--text-primary)', lineHeight: 1.6, overflow: 'auto', height: '100%' }}>
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{cellData.source || ''}</ReactMarkdown>
                    </div>
                  ) : (
                    <OutputRenderer
                      outputs={cellData.outputs}
                      fixedView={fixedView}
                      chartConfig={chartConfig}
                      footerExtra={cellData.updated_at ? (
                        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                          Executed at {formatExecutedAt(cellData.updated_at)}
                        </span>
                      ) : undefined}
                    />
                  )}
                </div>
              )
            })}
          </div>
        )}
      </main>
      <footer style={pageStyles.footer}>
        <span style={pageStyles.footerText}>Powered by Aether</span>
      </footer>
    </div>
  )

  if (!publicLive) return content
  return (
    <DashboardVariablesProvider
      dashboardId={dashboard.id}
      variables={variables}
      endpointBase={endpointBase}
      storageId={`pub_${token}`}
    >
      {content}
    </DashboardVariablesProvider>
  )
}

const pageStyles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: '100vh',
    background: 'var(--bg-primary)',
    display: 'flex',
    flexDirection: 'column',
  },
  header: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    minHeight: 56,
    flexShrink: 0,
    display: 'flex',
    alignItems: 'center',
  },
  headerInner: {
    width: '100%',
    padding: '0 40px',
    display: 'flex',
    alignItems: 'center',
    gap: 16,
  },
  brandMark: {
    fontSize: 15,
    fontWeight: 800,
    color: 'var(--nav-accent)',
    letterSpacing: '-0.02em',
    fontFamily: 'var(--font-mono)',
    flexShrink: 0,
  },
  titleBlock: {
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontSize: 16,
    fontWeight: 700,
    color: 'var(--nav-text)',
    margin: 0,
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  subtitle: {
    fontSize: 12,
    color: 'var(--nav-text-muted)',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  readOnlyBadge: {
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--nav-text-muted)',
    background: 'rgba(255,255,255,0.06)',
    border: '1px solid var(--nav-border)',
    padding: '2px 8px',
    borderRadius: 4,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    flexShrink: 0,
  },
  body: {
    flex: 1,
    padding: '40px 40px',
    width: '100%',
  },
  footer: {
    borderTop: '1px solid var(--border)',
    padding: '16px 40px',
    display: 'flex',
    justifyContent: 'center',
    flexShrink: 0,
  },
  footerText: {
    fontSize: 12,
    color: 'var(--text-muted)',
  },
}
