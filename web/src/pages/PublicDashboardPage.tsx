import { useRef, useState, useEffect, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api } from '../api/client'
import { Skeleton } from '../components/Skeleton'
import { OutputRenderer } from '../components/OutputRenderer'
import type { Dashboard, Widget, Output } from '../types'
import type { ChartConfig } from '../charts'

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

  const measureRef = useCallback((el: HTMLDivElement | null) => {
    gridRef.current = el
    if (el) {
      setContainerWidth(el.clientWidth)
    }
  }, [])

  useEffect(() => {
    const el = gridRef.current
    if (!el) return
    const ro = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setContainerWidth(entry.contentRect.width)
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  if (isLoading) return <div style={{ padding: 40 }}><Skeleton count={5} height={40} /></div>
  if (error) return <div style={{ padding: 40, color: 'var(--error)' }}>Not found or sharing disabled</div>
  if (!dashboard) return null

  const widgets = dashboard.widgets ?? []
  const widgetsData = dashboard.widgets_data ?? {}
  const cols = dashboard.settings?.grid_cols ?? 12
  const colWidth = (containerWidth - (cols - 1) * gap) / cols
  const totalHeight = widgets.reduce((max, w) => {
    return Math.max(max, (w.layout?.row ?? 0) + (w.layout?.height ?? 4))
  }, 0) * (ROW_HEIGHT + gap)

  return (
    <div style={pageStyles.page}>
      <header style={pageStyles.header}>
        <div style={pageStyles.headerInner}>
          <span style={pageStyles.brandMark}>Aether</span>
          <h1 style={pageStyles.title}>{dashboard.title}</h1>
          <span style={pageStyles.readOnlyBadge}>Read-only</span>
        </div>
      </header>
      <main style={pageStyles.body}>
        {widgets.length === 0 ? (
          <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
            <p style={{ fontSize: 14, fontWeight: 600, margin: '0 0 4px' }}>No widgets in this dashboard</p>
            <p style={{ fontSize: 13, margin: 0 }}>The dashboard owner hasn't added any widgets yet.</p>
          </div>
        ) : (
          <div ref={measureRef} style={{ minHeight: totalHeight, position: 'relative' }}>
            {widgets.map((widget) => {
              const cellData = widgetsData[widget.cell_id!]
              if (!cellData || !cellData.outputs?.length) return null

              const l = widget.layout
              const left = l.col * (colWidth + gap)
              const top = l.row * (ROW_HEIGHT + gap)
              const width = l.width * colWidth + (l.width - 1) * gap
              const height = l.height * ROW_HEIGHT + (l.height - 1) * gap

              const isChart = widget.type === 'chart'
              const fixedView = isChart ? 'chart' : 'table'
              const chartConfig = { ...(cellData.metadata?.chart || {}), ...(widget.config || {}) } as ChartConfig | undefined

              return (
                <div key={widget.id} style={{
                  position: 'absolute' as const,
                  left, top, width, height,
                  background: 'var(--bg-card)',
                  border: '1px solid var(--border)',
                  borderRadius: 4,
                  overflow: 'hidden',
                  display: 'flex',
                  flexDirection: 'column',
                }}>
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
                        <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>
                          Executed at {new Date(cellData.updated_at).toLocaleDateString([], { year: 'numeric', month: '2-digit', day: '2-digit' })} {new Date(cellData.updated_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
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
    height: 56,
    flexShrink: 0,
    display: 'flex',
    alignItems: 'center',
  },
  headerInner: {
    width: '100%',
    maxWidth: 1280,
    margin: '0 auto',
    padding: '0 40px',
    display: 'flex',
    alignItems: 'center',
    gap: 16,
  },
  brandMark: {
    fontSize: 15,
    fontWeight: 800,
    color: 'var(--accent)',
    letterSpacing: '-0.02em',
    fontFamily: 'var(--font-mono)',
    flexShrink: 0,
  },
  title: {
    fontSize: 16,
    fontWeight: 700,
    color: 'var(--nav-text)',
    margin: 0,
    flex: 1,
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  readOnlyBadge: {
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-muted)',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    padding: '2px 8px',
    borderRadius: 4,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    flexShrink: 0,
  },
  body: {
    flex: 1,
    maxWidth: 1280,
    margin: '0 auto',
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
