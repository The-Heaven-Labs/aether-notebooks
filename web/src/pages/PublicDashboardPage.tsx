import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import type { Dashboard, Widget } from '../types'
import { EmptyState } from '../components/EmptyState'

interface DashboardWithWidgets extends Dashboard {
  widgets: Widget[]
}

export function PublicDashboardPage() {
  const { token } = useParams<{ token: string }>()
  const [dashboard, setDashboard] = useState<DashboardWithWidgets | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!token) {
      setError('Invalid dashboard link')
      setLoading(false)
      return
    }

    fetch(`/api/v1/public/dashboards/${token}`)
      .then((res) => {
        if (res.status === 404) throw new Error('Dashboard not found or is not public')
        if (!res.ok) throw new Error(`Failed to load dashboard (${res.status})`)
        return res.json() as Promise<DashboardWithWidgets>
      })
      .then((data) => {
        setDashboard(data)
        setLoading(false)
      })
      .catch((err: Error) => {
        setError(err.message)
        setLoading(false)
      })
  }, [token])

  if (loading) {
    return (
      <div style={styles.centeredPage}>
        <div style={styles.loadingDot} />
      </div>
    )
  }

  if (error || !dashboard) {
    return (
      <div style={styles.centeredPage}>
        <div style={styles.errorBox}>
          <p style={styles.errorTitle}>Unable to load dashboard</p>
          <p style={styles.errorDetail}>{error ?? 'Dashboard not found'}</p>
        </div>
      </div>
    )
  }

  const widgets = dashboard.widgets ?? []

  return (
    <div style={styles.page}>
      <header style={styles.header}>
        <div style={styles.headerInner}>
          <span style={styles.brandMark}>hnb</span>
          <h1 style={styles.title}>{dashboard.title}</h1>
          <span style={styles.readOnlyBadge}>Read-only</span>
        </div>
      </header>

      <main style={styles.body}>
        {widgets.length === 0 ? (
          <EmptyState
            title="No widgets in this dashboard"
            text="The dashboard owner hasn't added any widgets yet."
          />
        ) : (
          <div style={styles.grid}>
            {widgets.map((widget) => (
              <div key={widget.id} style={styles.widgetCard}>
                <div style={styles.widgetHeader}>
                  <span style={styles.widgetTypeBadge}>{widget.type}</span>
                </div>
                <div style={styles.widgetBody}>
                  <div style={styles.widgetRef}>
                    <span style={styles.widgetRefLabel}>Notebook</span>
                    <code style={styles.widgetRefValue}>{widget.notebook_id.slice(0, 8)}…</code>
                  </div>
                  <div style={styles.widgetRef}>
                    <span style={styles.widgetRefLabel}>Cell</span>
                    <code style={styles.widgetRefValue}>{widget.cell_id.slice(0, 8)}…</code>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </main>

      <footer style={styles.footer}>
        <span style={styles.footerText}>Powered by hnb</span>
      </footer>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  centeredPage: {
    minHeight: '100vh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'var(--bg-primary)',
  },
  loadingDot: {
    width: 8,
    height: 8,
    borderRadius: '50%',
    background: 'var(--accent)',
    opacity: 0.5,
  },
  errorBox: {
    textAlign: 'center',
    padding: '32px 40px',
    background: 'white',
    borderRadius: 12,
    boxShadow: 'var(--shadow-sm)',
    border: '1px solid var(--border)',
  },
  errorTitle: {
    fontSize: 16,
    fontWeight: 700,
    color: 'var(--text-primary)',
    margin: '0 0 8px',
  },
  errorDetail: {
    fontSize: 13,
    color: 'var(--text-muted)',
    margin: 0,
  },
  page: {
    minHeight: '100vh',
    background: 'var(--bg-primary)',
    display: 'flex',
    flexDirection: 'column',
  },
  header: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    height: 56,
    flexShrink: 0,
    display: 'flex',
    alignItems: 'center',
  },
  headerInner: {
    width: '100%',
    maxWidth: 1280,
    margin: '0 auto',
    padding: '0 40px',
    display: 'flex',
    alignItems: 'center',
    gap: 16,
  },
  brandMark: {
    fontSize: 15,
    fontWeight: 800,
    color: 'var(--accent)',
    letterSpacing: '-0.02em',
    fontFamily: 'var(--font-mono)',
    flexShrink: 0,
  },
  title: {
    fontSize: 16,
    fontWeight: 700,
    color: 'var(--nav-text)',
    margin: 0,
    flex: 1,
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  readOnlyBadge: {
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-muted)',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    padding: '2px 8px',
    borderRadius: 4,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    flexShrink: 0,
  },
  body: {
    flex: 1,
    maxWidth: 1280,
    margin: '0 auto',
    padding: '40px 40px',
    width: '100%',
  },
  grid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(2, 1fr)',
    gap: 16,
  },
  widgetCard: {
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 10,
    boxShadow: 'var(--shadow-sm)',
    overflow: 'hidden',
  },
  widgetHeader: {
    display: 'flex',
    alignItems: 'center',
    padding: '10px 14px',
    borderBottom: '1px solid var(--border)',
    background: 'var(--bg-secondary)',
  },
  widgetTypeBadge: {
    fontSize: 11,
    fontWeight: 700,
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
    color: 'var(--accent)',
    background: '#f0edff',
    padding: '2px 8px',
    borderRadius: 4,
  },
  widgetBody: {
    padding: '14px 16px',
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  widgetRef: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  widgetRefLabel: {
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-muted)',
    textTransform: 'uppercase',
    letterSpacing: '0.04em',
    width: 60,
    flexShrink: 0,
  },
  widgetRefValue: {
    fontSize: 12,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
    background: 'var(--bg-secondary)',
    padding: '2px 6px',
    borderRadius: 4,
  },
  footer: {
    borderTop: '1px solid var(--border)',
    padding: '16px 40px',
    display: 'flex',
    justifyContent: 'center',
    flexShrink: 0,
  },
  footerText: {
    fontSize: 12,
    color: 'var(--text-muted)',
  },
}
