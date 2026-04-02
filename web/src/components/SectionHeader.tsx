import type React from 'react'

interface Props {
  title: string
  subtitle?: string
  children?: React.ReactNode
}

export function SectionHeader({ title, subtitle, children }: Props) {
  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', marginBottom: 24 }}>
      <div>
        <h2 style={{ fontSize: 22, fontWeight: 700, letterSpacing: '-0.3px', color: 'var(--text-primary)', margin: 0 }}>
          {title}
        </h2>
        {subtitle && (
          <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 2, marginBottom: 0 }}>
            {subtitle}
          </p>
        )}
      </div>
      {children && <div>{children}</div>}
    </div>
  )
}
