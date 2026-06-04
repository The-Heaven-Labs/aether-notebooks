import type React from 'react'

interface SkeletonProps {
  width?: string | number
  height?: string | number
  borderRadius?: number
  style?: React.CSSProperties
  count?: number
}

export function Skeleton({ width = '100%', height = 16, borderRadius = 4, style, count = 1 }: SkeletonProps) {
  const base: React.CSSProperties = {
    width,
    height,
    borderRadius,
    background: 'var(--border-light)',
    animation: 'skeleton-pulse 1.5s ease-in-out infinite',
    ...style,
  }
  if (count === 1) return <div style={base} />
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {Array.from({ length: count }, (_, i) => <div key={i} style={base} />)}
    </div>
  )
}
