import type React from 'react'

interface Props {
  headers: string[]
  children: React.ReactNode
  thStyle?: React.CSSProperties
}

const tableWrapStyle: React.CSSProperties = {
  borderRadius: 4,
  overflow: 'hidden',
  border: '1px solid #e8e8e8',
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
  fontWeight: 600,
  color: '#888',
  letterSpacing: '0.06em',
  borderBottom: '1px solid #e8e8e8',
  background: '#fff',
  fontFamily: 'var(--font-mono)',
  textTransform: 'uppercase',
}

export const rowStyle: React.CSSProperties = { borderBottom: '1px solid #e8e8e8' }
export const cellStyle: React.CSSProperties = { padding: '12px 16px', fontSize: 13, color: '#333' }

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
