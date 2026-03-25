import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Schedule, Parameter } from '../types'

interface Props {
  notebookId: string
  parameters: Parameter[]
}

export function SchedulesPanel({ notebookId, parameters: _parameters }: Props) {
  const qc = useQueryClient()
  const [cronDraft, setCronDraft] = useState('')
  const [createError, setCreateError] = useState<string | null>(null)
  const [mutationErrors, setMutationErrors] = useState<Record<string, string>>({})

  const { data: schedules = [], isLoading, isError } = useQuery({
    queryKey: ['schedules', notebookId],
    queryFn: () => api.get<Schedule[]>(`/api/v1/notebooks/${notebookId}/schedules`),
    enabled: !!notebookId,
  })

  const createSchedule = useMutation({
    mutationFn: (cron_expression: string) =>
      api.post<Schedule>(`/api/v1/notebooks/${notebookId}/schedules`, {
        cron_expression,
        parameter_overrides: {},
      }),
    onSuccess: () => {
      setCronDraft('')
      setCreateError(null)
      qc.invalidateQueries({ queryKey: ['schedules', notebookId] })
    },
    onError: (err: unknown) => {
      setCreateError(err instanceof Error ? err.message : 'Failed to create schedule')
    },
  })

  const toggleSchedule = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.put<Schedule>(`/api/v1/schedules/${id}`, { enabled }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['schedules', notebookId] })
    },
    onError: (err: unknown, vars) => {
      setMutationErrors((prev) => ({
        ...prev,
        [vars.id]: err instanceof Error ? err.message : 'Failed to toggle schedule',
      }))
    },
  })

  const deleteSchedule = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/schedules/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['schedules', notebookId] })
    },
    onError: (err: unknown, id) => {
      setMutationErrors((prev) => ({
        ...prev,
        [id]: err instanceof Error ? err.message : 'Failed to delete schedule',
      }))
    },
  })

  const handleCreate = () => {
    const trimmed = cronDraft.trim()
    if (!trimmed) {
      setCreateError('Cron expression is required')
      return
    }
    createSchedule.mutate(trimmed)
  }

  const handleDelete = (id: string) => {
    if (!window.confirm('Delete this schedule?')) return
    setMutationErrors((prev) => {
      const next = { ...prev }
      delete next[id]
      return next
    })
    deleteSchedule.mutate(id)
  }

  const handleToggle = (schedule: Schedule) => {
    setMutationErrors((prev) => {
      const next = { ...prev }
      delete next[schedule.id]
      return next
    })
    toggleSchedule.mutate({ id: schedule.id, enabled: !schedule.enabled })
  }

  const formatDate = (val: string | null) => {
    if (!val) return 'N/A'
    try {
      return new Date(val).toLocaleString()
    } catch {
      return val
    }
  }

  return (
    <div style={styles.panel}>
      <div style={styles.header}>
        <span style={styles.headerTitle}>Schedules</span>
      </div>

      <div style={styles.content}>
        {/* Create form */}
        <div style={styles.createForm}>
          <input
            style={styles.cronInput}
            type="text"
            placeholder="Cron expression (e.g. 0 9 * * 1)"
            value={cronDraft}
            onChange={(e) => {
              setCronDraft(e.target.value)
              setCreateError(null)
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleCreate()
            }}
          />
          <button
            type="button"
            style={styles.createBtn}
            onClick={handleCreate}
            disabled={createSchedule.isPending}
          >
            {createSchedule.isPending ? 'Creating…' : 'Create'}
          </button>
        </div>
        {createError && <div style={styles.errorText}>{createError}</div>}

        {/* Schedule list */}
        {isLoading ? (
          <div style={styles.statusRow}>
            <div style={styles.loadingDot} />
            <span style={styles.statusText}>Loading schedules…</span>
          </div>
        ) : isError ? (
          <div style={styles.errorText}>Failed to load schedules</div>
        ) : schedules.length === 0 ? (
          <div style={styles.emptyText}>No schedules yet. Create one above.</div>
        ) : (
          <div style={styles.list}>
            {schedules.map((schedule) => (
              <div key={schedule.id} style={styles.scheduleItem}>
                <div style={styles.scheduleTop}>
                  <span style={styles.cronText}>{schedule.cron_expression}</span>
                  <span style={{ ...styles.enabledBadge, ...(schedule.enabled ? styles.badgeOn : styles.badgeOff) }}>
                    {schedule.enabled ? 'enabled' : 'disabled'}
                  </span>
                  <button
                    type="button"
                    style={styles.toggleBtn}
                    onClick={() => handleToggle(schedule)}
                    disabled={toggleSchedule.isPending}
                    title={schedule.enabled ? 'Disable schedule' : 'Enable schedule'}
                  >
                    {schedule.enabled ? 'Disable' : 'Enable'}
                  </button>
                  <button
                    type="button"
                    style={styles.deleteBtn}
                    onClick={() => handleDelete(schedule.id)}
                    disabled={deleteSchedule.isPending}
                    title="Delete schedule"
                  >
                    Delete
                  </button>
                </div>
                <div style={styles.scheduleBottom}>
                  <span style={styles.metaText}>
                    Next run: {formatDate(schedule.next_run_at)}
                  </span>
                  <span style={styles.metaSep}>·</span>
                  <span style={styles.metaText}>
                    Created: {formatDate(schedule.created_at)}
                  </span>
                </div>
                {mutationErrors[schedule.id] && (
                  <div style={styles.errorText}>{mutationErrors[schedule.id]}</div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: {
    background: 'var(--bg-secondary)',
    borderTop: '1px solid var(--border)',
    display: 'flex',
    flexDirection: 'column',
    flexShrink: 0,
    maxHeight: 360,
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '10px 24px',
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  headerTitle: {
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
  },
  content: {
    flex: 1,
    overflowY: 'auto',
    padding: '16px 24px',
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
  createForm: {
    display: 'flex',
    gap: 8,
    alignItems: 'center',
  },
  cronInput: {
    flex: 1,
    padding: '6px 10px',
    background: 'var(--bg-primary)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    color: 'var(--text-primary)',
    fontSize: 13,
    fontFamily: 'var(--font-mono)',
    outline: 'none',
  },
  createBtn: {
    padding: '6px 16px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    flexShrink: 0,
    letterSpacing: '0.01em',
  },
  errorText: {
    fontSize: 12,
    color: '#c0392b',
    marginTop: 2,
  },
  statusRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '8px 0',
  },
  loadingDot: {
    width: 7,
    height: 7,
    borderRadius: '50%',
    background: 'var(--accent)',
    opacity: 0.5,
    flexShrink: 0,
  },
  statusText: {
    fontSize: 12,
    color: 'var(--text-muted)',
  },
  emptyText: {
    fontSize: 13,
    color: 'var(--text-muted)',
    padding: '8px 0',
  },
  list: {
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
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
    color: '#c0392b',
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
}
