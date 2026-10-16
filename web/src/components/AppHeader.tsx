import type { CSSProperties, ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, Loader2, RefreshCw } from 'lucide-react'
import { formatExecutedAt } from '../utils/formatDateTime'
import { appContentWidthStyle } from '../utils/appLayout'

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
  /** App logo image URL; renders before the title block when set. */
  logoUrl?: string
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
  logoUrl,
  children,
}: AppHeaderProps) {
  return (
    <header className="app-header" style={styles.root}>
      <div
        className="app-header-content"
        style={appContentWidthStyle(width)}
      >
        {backTo && (
          <Link
            to={backTo}
            aria-label="Back to dashboards"
            title="Back to dashboards"
            className="icon-btn"
            style={styles.backLink}
          >
            <ArrowLeft size={16} />
          </Link>
        )}
        {logoUrl && (
          // Decorative: the adjacent h1 announces the app name.
          <img src={logoUrl} alt="" style={styles.logo} />
        )}
        {title && (
          <div style={styles.titleBlock}>
            <h1 style={styles.title}>{title}</h1>
            {subtitle && <div style={styles.subtitle}>{subtitle}</div>}
          </div>
        )}
        <div className="app-header-actions">
          {showLastUpdated && (
            <span
              role="status"
              aria-live="polite"
              className="app-header-freshness"
            >
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
  // Padding lives on the .app-header class (theme.css) so the responsive
  // media rule can tighten it; the hairline stays inline with the component.
  root: {
    borderBottom: '1px solid var(--border-light)',
  },
  // The app's own mark, sized to sit quietly beside the title block.
  logo: {
    height: 24,
    maxWidth: 160,
    objectFit: 'contain',
    flexShrink: 0,
    display: 'block',
  },
  // 36×36: the app surface's ≥36px tap-target rule (The Story).
  backLink: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 36,
    height: 36,
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
    // A floor so a wide action row (e.g. draft preview's LIVE + DRAFT PREVIEW +
    // View published) wraps the masthead instead of crushing the title to an
    // ellipsis. Kept small enough that ordinary rows stay single-line.
    minWidth: 220,
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
  // 36×36: same tap-target rule as the back link.
  refreshButton: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 36,
    height: 36,
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
