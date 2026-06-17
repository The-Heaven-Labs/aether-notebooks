import type React from 'react'

export function ConfigHint({ children }: { children: React.ReactNode }) {
  return (
    <div style={hintStyle}>
      {children}
    </div>
  )
}

const hintStyle: React.CSSProperties = {
  fontSize: 11,
  color: 'var(--text-muted)',
  lineHeight: 1.4,
  marginTop: 2,
}
