import type React from 'react'

interface Props {
  headers: string[]
  children: React.ReactNode
  thStyle?: React.CSSProperties
}

const tableWrapStyle: React.CSSProperties = {
  borderRadius: 10,
  overflow: 'hidden',
  border: '1px solid var(--border)',
  boxShadow: 'var(--shadow-sm)',
}

const tableStyle: React.CSSProperties = {
  width: '100%',
  borderCollapse: 'collapse',
  background: 'white',
}

const thBase: React.CSSProperties = {
  padding: '10px 16px',
  textAlign: 'left',
  fontSize: 11,
  fontWeight: 700,
  color: 'var(--text-muted)',
  letterSpacing: '0.06em',
  borderBottom: '1px solid var(--border-light)',
  background: 'var(--bg-secondary)',
  textTransform: 'uppercase',
}

export const rowStyle: React.CSSProperties = { borderBottom: '1px solid var(--border-light)' }
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
