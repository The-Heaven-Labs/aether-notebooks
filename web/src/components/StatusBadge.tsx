import type React from 'react'

type Status = 'success' | 'error' | 'neutral'

interface Props {
  status: Status
  label: string
  icon?: React.ReactNode
}

const colorMap: Record<Status, string> = {
  success: 'var(--success)',
  error: 'var(--error-full)',
  neutral: 'var(--text-muted)',
}

export function StatusBadge({ status, label, icon }: Props) {
  const style: React.CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 12,
    fontWeight: 600,
    color: colorMap[status],
  }

  return (
    <span style={style} role="status" aria-live="polite">
      {icon}
      {label}
    </span>
  )
}
