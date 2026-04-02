import type { Schedule } from '../types'

interface Props {
  schedule: Schedule
  onToggle: (schedule: Schedule) => void
  onDelete: (id: string) => void
  togglePending?: boolean
  deletePending?: boolean
  error?: string
}

function formatDate(val: string | null) {
  if (!val) return 'N/A'
  try { return new Date(val).toLocaleString() } catch { return val ?? 'N/A' }
}

export function ScheduleItem({ schedule, onToggle, onDelete, togglePending, deletePending, error }: Props) {
  return (
    <div style={styles.scheduleItem}>
      <div style={styles.scheduleTop}>
        <span style={styles.cronText}>{schedule.cron_expression}</span>
        <span style={{ ...styles.enabledBadge, ...(schedule.enabled ? styles.badgeOn : styles.badgeOff) }}>
          {schedule.enabled ? 'enabled' : 'disabled'}
        </span>
        <button
          type="button"
          style={styles.toggleBtn}
          onClick={() => onToggle(schedule)}
          disabled={togglePending}
          title={schedule.enabled ? 'Disable schedule' : 'Enable schedule'}
        >
          {schedule.enabled ? 'Disable' : 'Enable'}
        </button>
        <button
          type="button"
          style={styles.deleteBtn}
          onClick={() => onDelete(schedule.id)}
          disabled={deletePending}
          title="Delete schedule"
        >
          Delete
        </button>
      </div>
      <div style={styles.scheduleBottom}>
        <span style={styles.metaText}>Next run: {formatDate(schedule.next_run_at)}</span>
        <span style={styles.metaSep}>·</span>
        <span style={styles.metaText}>Created: {formatDate(schedule.created_at)}</span>
      </div>
      {error && (
        <div style={styles.errorText}>{error}</div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  scheduleItem: {
    background: 'var(--bg-primary)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    padding: '10px 14px',
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  scheduleTop: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    flexWrap: 'wrap',
  },
  cronText: {
    fontFamily: 'var(--font-mono)',
    fontSize: 13,
    color: 'var(--text-primary)',
    fontWeight: 500,
    flex: 1,
    minWidth: 0,
  },
  enabledBadge: {
    fontSize: 11,
    fontWeight: 600,
    borderRadius: 10,
    padding: '2px 8px',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    flexShrink: 0,
  },
  badgeOn: {
    background: 'rgba(39, 174, 96, 0.15)',
    color: '#27ae60',
  },
  badgeOff: {
    background: 'var(--border)',
    color: 'var(--text-muted)',
  },
  toggleBtn: {
    padding: '4px 10px',
    background: 'transparent',
    border: '1px solid var(--border)',
    borderRadius: 5,
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    flexShrink: 0,
  },
  deleteBtn: {
    padding: '4px 10px',
    background: 'transparent',
    border: '1px solid rgba(192, 57, 43, 0.4)',
    borderRadius: 5,
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--error-full)',
    cursor: 'pointer',
    flexShrink: 0,
  },
  scheduleBottom: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  metaText: {
    fontSize: 11,
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
  },
  metaSep: {
    fontSize: 11,
    color: 'var(--text-muted)',
  },
  errorText: {
    fontSize: 12,
    color: 'var(--error-full)',
    marginTop: 2,
  },
}
