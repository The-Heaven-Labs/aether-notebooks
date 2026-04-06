import type React from 'react'

interface Props {
  headers: string[]
  children: React.ReactNode
  thStyle?: React.CSSProperties
}

const tableWrapStyle: React.CSSProperties = {
  borderRadius: 4,
  overflow: 'hidden',
  border: '1px solid var(--border)',
}

const tableStyle: React.CSSProperties = {
  width: '100%',
  borderCollapse: 'collapse',
  background: 'var(--bg-card)',
}

const thBase: React.CSSProperties = {
  padding: '10px 16px',
  textAlign: 'left',
  fontSize: 11,
  fontWeight: 600,
  color: 'var(--text-muted)',
  letterSpacing: '0.06em',
  borderBottom: '1px solid var(--border)',
  background: 'var(--bg-card)',
  fontFamily: 'var(--font-mono)',
  textTransform: 'uppercase',
}

export const rowStyle: React.CSSProperties = { borderBottom: '1px solid var(--border)' }
export const cellStyle: React.CSSProperties = { padding: '12px 16px', fontSize: 13, color: 'var(--text-primary)' }

export function StyledTable({ headers, children, thStyle }: Props) {
  return (
    <div style={tableWrapStyle}>
      <table style={tableStyle}>
        <thead>
          <tr>
            {headers.map((h) => (
              <th key={h} style={{ ...thBase, ...thStyle }}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {children}
        </tbody>
      </table>
    </div>
  )
}
