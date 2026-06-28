import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { api } from '../api/client'
import { Skeleton } from '../components/Skeleton'
import type { Notebook, Parameter } from '../types'

interface PublicCell {
  position: number
  type: string
  language?: string
  source: string
  outputs: Output[]
  parameters?: Parameter[]
}

interface Output {
  type: string
  data: unknown
  config?: unknown
}

export function PublicNotebookPage() {
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
        {data.cells.map(cell => (
          <div key={cell.position} style={{ marginBottom: 16 }}>
            {cell.type === 'text' ? (
              <div style={{ padding: '8px 0', fontSize: 14, color: 'var(--text-primary)', lineHeight: 1.6 }}>
                <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}>{cell.source}</ReactMarkdown>
              </div>
            ) : (
              <>
                {cell.source && (
                  <pre style={{ background: 'var(--bg-code)', padding: 12, borderRadius: 4, overflow: 'auto', fontSize: 13, color: 'var(--text-primary)', margin: 0 }}>{cell.source}</pre>
                )}
                {cell.outputs?.map((out, i) => (
                  <div key={i} style={{ marginTop: 8 }}>
                    {out.type === 'text' && typeof out.data === 'string' && (
                      <pre style={{ background: 'var(--bg-secondary)', padding: 10, borderRadius: 4, fontSize: 13, color: 'var(--text-primary)', whiteSpace: 'pre-wrap' }}>{out.data}</pre>
                    )}
                    {out.type === 'error' && typeof out.data === 'string' && (
                      <pre style={{ background: '#fef2f2', padding: 10, borderRadius: 4, fontSize: 13, color: '#dc2626', whiteSpace: 'pre-wrap' }}>{out.data}</pre>
                    )}
                    {out.type === 'table' && out.data && typeof out.data === 'object' && (
                      <TableView data={out.data as TableData} />
                    )}
                  </div>
                ))}
              </>
            )}
          </div>
        ))}
      </main>
      <footer style={s.footer}>
        <span style={s.footerText}>Powered by hnb</span>
      </footer>
    </div>
  )
}

interface TableData {
  columns: { name: string; type: string }[]
  rows: Record<string, unknown>[]
}

function TableView({ data }: { data: TableData }) {
  if (!data.columns || !data.rows) return null
  return (
    <div style={{ overflowX: 'auto', border: '1px solid var(--border)', borderRadius: 4 }}>
      <table style={{ borderCollapse: 'collapse', fontSize: 13, width: '100%' }}>
        <thead>
          <tr style={{ background: 'var(--bg-secondary)' }}>
            {data.columns.map(col => (
              <th key={col.name} style={{ padding: '6px 10px', textAlign: 'left', fontWeight: 600, color: 'var(--text-primary)', borderBottom: '1px solid var(--border)', whiteSpace: 'nowrap' }}>{col.name}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.rows.slice(0, 100).map((row, i) => (
            <tr key={i} style={{ background: i % 2 === 0 ? 'transparent' : 'var(--bg-secondary)' }}>
              {data.columns.map(col => (
                <td key={col.name} style={{ padding: '4px 10px', color: 'var(--text-primary)', borderBottom: '1px solid var(--border)', whiteSpace: 'nowrap' }}>{String(row[col.name] ?? '')}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {data.rows.length > 100 && (
        <div style={{ padding: '8px 10px', fontSize: 12, color: 'var(--text-muted)', borderTop: '1px solid var(--border)', textAlign: 'center' }}>
          Showing 100 of {data.rows.length} rows
        </div>
      )}
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
