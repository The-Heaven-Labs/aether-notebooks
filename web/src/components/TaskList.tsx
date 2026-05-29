import { Check, Circle, Loader2 } from 'lucide-react'
import type { AgentTaskItem } from '../types/agent'

interface Props {
  tasks: AgentTaskItem[]
}

function statusIcon(status: string) {
  switch (status) {
    case 'done': return <Check size={12} style={{ color: 'var(--accent)' }} />
    case 'in_progress': return <Loader2 size={12} style={{ animation: 'spin 1s linear infinite', color: 'var(--accent)' }} />
    default: return <Circle size={12} style={{ color: 'var(--text-muted)' }} />
  }
}

export function TaskList({ tasks }: Props) {
  if (tasks.length === 0) return null

  const doneCount = tasks.filter(t => t.status === 'done').length

  return (
    <details open style={styles.container}>
      <summary style={styles.header}>Tasks ({doneCount}/{tasks.length})</summary>
      <div style={styles.list}>
        {tasks.map((task) => (
          <div key={task.id} style={{
            ...styles.item,
            ...(task.status === 'done' ? styles.doneItem : {}),
          }}>
            {statusIcon(task.status)}
            <span style={{
              ...styles.description,
              ...(task.status === 'done' ? styles.doneText : {}),
            }}>
              {task.description}
            </span>
          </div>
        ))}
      </div>
    </details>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    padding: '8px 12px',
    background: 'var(--bg-secondary)',
    borderBottom: '1px solid var(--border-light)',
    fontSize: 12,
  },
  header: {
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 11,
    fontWeight: 500,
    marginBottom: 4,
  },
  list: {
    display: 'flex',
    flexDirection: 'column',
    gap: 4,
  },
  item: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '2px 0',
  },
  doneItem: {
    opacity: 0.5,
  },
  description: {
    color: 'var(--text-primary)',
    fontSize: 12,
  },
  doneText: {
    textDecoration: 'line-through',
  },
}
