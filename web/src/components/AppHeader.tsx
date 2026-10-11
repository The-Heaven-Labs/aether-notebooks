import type { CSSProperties, ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, Loader2, RefreshCw } from 'lucide-react'
import { formatExecutedAt } from '../utils/formatDateTime'

interface AppHeaderProps {
  /** Masthead title (the app title, falling back to the dashboard title). */
  title: string
  subtitle?: string
  /** `fixed` constrains content to 1200px; `full` spans the viewport. */
  width: 'fixed' | 'full'
  showRefresh: boolean
  showLastUpdated: boolean
  lastUpdated: Date | null
  refreshing: boolean
  onRefresh: () => void
  /** When set, a back link to this route renders before the title block. */
  backTo?: string
  /** Right-side actions (Edit, Share, connector selector, …). */
  children?: ReactNode
}

/**
 * The App surface masthead (The Story's first viewport): a quiet strip with
 * the title block on the left and freshness, refresh, and caller-supplied
 * actions on the right. Deliberately not sticky — the app reads as a written
 * brief, not a floating console.
 */
export function AppHeader({
  title,
  subtitle,
  width,
  showRefresh,
  showLastUpdated,
  lastUpdated,
  refreshing,
  onRefresh,
  backTo,
  children,
}: AppHeaderProps) {
  return (
    <header style={styles.root}>
      <div style={width === 'fixed' ? styles.contentFixed : styles.content}>
        {backTo && (
          <Link to={backTo} aria-label="Back to dashboards" title="Back to dashboards" style={styles.backLink}>
            <ArrowLeft size={16} />
          </Link>
        )}
        {title && (
          <div style={styles.titleBlock}>
            <h1 style={styles.title}>{title}</h1>
            {subtitle && <div style={styles.subtitle}>{subtitle}</div>}
          </div>
        )}
        <div style={styles.actions}>
          {showLastUpdated && (
            <span role="status" aria-live="polite" style={styles.freshness}>
              {lastUpdated ? `Updated ${formatExecutedAt(lastUpdated)}` : ''}
            </span>
          )}
          {showRefresh && (
            <button
              type="button"
              onClick={onRefresh}
              disabled={refreshing}
              title="Refresh data"
              aria-label="Refresh data"
              style={refreshing ? { ...styles.refreshButton, ...styles.refreshButtonBusy } : styles.refreshButton}
            >
              {refreshing ? (
                <Loader2 size={14} style={{ animation: 'spin 1s linear infinite' }} />
              ) : (
                <RefreshCw size={14} />
              )}
            </button>
          )}
          {children}
        </div>
      </div>
    </header>
  )
}

const styles: Record<string, CSSProperties> = {
  root: {
    padding: '16px 32px',
    borderBottom: '1px solid var(--border-light)',
  },
  content: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
  },
  contentFixed: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    maxWidth: 1200,
    margin: '0 auto',
  },
  backLink: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 30,
    height: 30,
    flexShrink: 0,
    borderRadius: 4,
    color: 'var(--text-muted)',
    textDecoration: 'none',
  },
  titleBlock: {
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    flex: 1,
    minWidth: 0,
  },
  title: {
    margin: 0,
    fontFamily: 'var(--font-sans)',
    fontSize: 22,
    fontWeight: 700,
    letterSpacing: '-0.3px',
    lineHeight: 1.2,
    color: 'var(--text-primary)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  subtitle: {
    fontSize: 13,
    lineHeight: 1.4,
    color: 'var(--text-secondary)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  actions: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    flexShrink: 0,
    marginLeft: 'auto',
  },
  freshness: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap',
  },
  refreshButton: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 30,
    height: 30,
    padding: 0,
    borderRadius: 4,
    border: '1px solid var(--border)',
    background: 'var(--bg-card)',
    color: 'var(--text-muted)',
    cursor: 'pointer',
  },
  refreshButtonBusy: {
    opacity: 0.6,
    cursor: 'default',
  },
}
