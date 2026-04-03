import type React from 'react'

interface Props {
  title: string
  children: React.ReactNode
}

export function FormCard({ title, children }: Props) {
  return (
    <div style={cardStyle}>
      <h3 style={titleStyle}>{title}</h3>
      {children}
    </div>
  )
}

const cardStyle: React.CSSProperties = {
  background: '#fff',
  border: '1px solid #e8e8e8',
  borderRadius: 4,
  padding: 24,
  marginBottom: 24,
}

const titleStyle: React.CSSProperties = {
  margin: '0 0 16px',
  fontSize: 15,
  fontWeight: 700,
  color: 'var(--text-primary)',
}
