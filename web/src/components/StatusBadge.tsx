import type React from 'react'

type Status = 'success' | 'error' | 'neutral'

interface Props {
  status: Status
  label: string
  icon?: React.ReactNode
  /** Longer explanation surfaced as a tooltip and to assistive tech. */
  title?: string
}

const colorMap: Record<Status, string> = {
  success: 'var(--success)',
  error: 'var(--error-full)',
  neutral: 'var(--text-muted)',
}

export function StatusBadge({ status, label, icon, title }: Props) {
  const style: React.CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 12,
    fontWeight: 600,
    color: colorMap[status],
  }

  return (
    <span
      style={style}
      role="status"
      aria-live="polite"
      title={title}
      aria-label={title ? `${label}: ${title}` : undefined}
    >
      {icon}
      {label}
    </span>
  )
}
