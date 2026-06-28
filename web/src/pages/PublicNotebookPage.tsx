import { useMemo } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { api } from '../api/client'
import { Skeleton } from '../components/Skeleton'
import { OutputRenderer } from '../components/OutputRenderer'
import { makeMarkdownComponents } from '../components/MarkdownCell'
import type { Notebook, Parameter, Output, ResultSet } from '../types'
import type { ChartConfig } from '../charts'

interface PublicCell {
  position: number
  type: string
  language?: string
  source: string
  outputs: Output[]
  parameters?: Parameter[]
  metadata?: Record<string, unknown>
  outputs_hidden: boolean
}

export function PublicNotebookPage() {
  const mdComponents = useMemo(() => makeMarkdownComponents(() => {}, true), [])
  const { token } = useParams<{ token: string }>()
  const { data, isLoading, error } = useQuery({
    queryKey: ['public', token],
    queryFn: () => api.get<{ type: string; notebook: Notebook; cells: PublicCell[] }>(`/api/v1/public/${token}`),
    enabled: !!token,
  })

  if (isLoading) return <div style={{ padding: 40 }}><Skeleton count={5} height={40} /></div>
  if (error) return <div style={{ padding: 40, color: 'var(--error)' }}>Not found or sharing disabled</div>
  if (!data) return null

  return (
    <div style={s.page}>
      <header style={s.header}>
        <div style={s.headerInner}>
          <span style={s.brandMark}>hnb</span>
          <h1 style={s.title}>{data.notebook.title}</h1>
          <span style={s.readOnlyBadge}>Read-only</span>
        </div>
      </header>
      <main style={{ maxWidth: 900, margin: '0 auto', padding: '24px 32px' }}>
        {data.notebook.description && (
          <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24 }}>{data.notebook.description}</p>
        )}
        {data.cells.map(cell => {
          const isCode = cell.type === 'code'
          const meta = cell.metadata ?? {}
          const chartConfig = meta.chart as ChartConfig | undefined
          const viewMode = meta.viewMode as 'table' | 'chart' | undefined
          return (
            <div key={cell.position} style={{
              ...s.cell,
              borderLeft: `3px solid ${isCode ? 'var(--accent)' : 'var(--success)'}`,
            }}>
              {cell.type === 'text' ? (
                <div style={s.mdContainer}>
                  <div style={s.mdPreview}>
                    <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]} components={mdComponents}>{cell.source}</ReactMarkdown>
                  </div>
                </div>
              ) : (
                <>
                  {cell.source && (
                    <div style={s.codeEditor}>
                      <pre style={{ margin: 0, fontFamily: 'var(--font-mono)', fontSize: 13, lineHeight: 1.65, color: 'var(--text-primary)' }}>{cell.source}</pre>
                    </div>
                  )}
                  {!cell.outputs_hidden && cell.outputs.length > 0 && (
                    <div style={s.outputWrap}>
                      <OutputRenderer
                        outputs={cell.outputs}
                        chartConfig={chartConfig}
                        viewMode={viewMode}
                        hideExport
                        fixedView={viewMode === 'chart' ? 'chart' : undefined}
                      />
                    </div>
                  )}
                </>
              )}
            </div>
          )
        })}
      </main>
      <footer style={s.footer}>
        <span style={s.footerText}>Powered by hnb</span>
      </footer>
    </div>
  )
}

const s: Record<string, React.CSSProperties> = {
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
  cell: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    overflow: 'hidden',
    marginBottom: 12,
  },
  codeEditor: {
    borderTop: '1px solid var(--border-light)',
    borderBottom: '1px solid var(--border-light)',
    padding: '14px 16px',
    background: 'var(--cm-editor-bg)',
    fontFamily: 'var(--font-mono)',
    fontSize: 13,
    lineHeight: 1.65,
    color: 'var(--text-primary)',
    overflow: 'auto',
  },
  mdContainer: {
    borderTop: '1px solid var(--border-light)',
    borderBottom: '1px solid var(--border-light)',
    padding: '14px 20px',
    fontSize: 14,
    lineHeight: 1.75,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
  },
  mdPreview: {
    minHeight: 48,
  },
  outputWrap: {
    borderBottom: '1px solid var(--border-light)',
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
