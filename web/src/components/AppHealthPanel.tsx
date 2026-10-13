import { useRef } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useFocusTrap } from '../hooks/useFocusTrap'

/** One cohesion finding from GET /dashboards/{id}/review. */
export interface ReviewFinding {
  severity: 'error' | 'warning' | 'info'
  category: 'layout' | 'content' | 'surface'
  chapter_id?: string
  widget_id?: string
  message: string
  fix_tool?: string
}

/** The review report: findings plus the app links. */
export interface ReviewReport {
  dashboard_id: string
  findings: ReviewFinding[]
  count: number
  view_url: string
  preview_url: string
  public_url?: string
}

/** The quiet "fix" hint per finding, named by the agent tool that resolves it. */
const FIX_HINTS: Record<string, string> = {
  set_widget_config: 'Configure the widget',
  set_dashboard_layout: 'Arrange the layout',
  update_dashboard_settings: 'Open App settings',
  publish_dashboard: 'Publish the draft',
}

/** Highlight applied to the jumped-to widget; cleared after the flash. */
export const WIDGET_HIGHLIGHT_CLASS = 'dash-widget-highlight'
const HIGHLIGHT_MS = 1600
/** Grace period after asking the editor to switch chapters before retrying the jump. */
const CHAPTER_SWITCH_MS = 120

const SEVERITY_GROUPS: Array<{ severity: ReviewFinding['severity']; label: string }> = [
  { severity: 'error', label: 'Errors' },
  { severity: 'warning', label: 'Warnings' },
  { severity: 'info', label: 'Notes' },
]

const styles: Record<string, React.CSSProperties> = {
  // Matches the App settings / version drawers: the panel covers the top bar
  // (z-index 1550/1600), so it must sit above it.
  backdrop: {
    position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.3)', zIndex: 1700,
  },
  panel: {
    position: 'fixed', top: 0, right: 0, bottom: 0, width: 420, maxWidth: '100vw',
    background: 'var(--bg-card)', borderLeft: '1px solid var(--border)',
    display: 'flex', flexDirection: 'column', zIndex: 1701,
    boxShadow: 'var(--shadow-lg, -4px 0 16px rgba(0,0,0,0.2))',
  },
  header: {
    display: 'flex', alignItems: 'center', justifyContent: 'space-between',
    padding: '14px 16px', borderBottom: '1px solid var(--border)', flexShrink: 0,
  },
  title: { fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' },
  close: {
    background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)',
    padding: '2px 4px', display: 'flex', alignItems: 'center',
  },
  body: { padding: 16, display: 'flex', flexDirection: 'column', gap: 12, overflowY: 'auto', flex: 1 },
  note: { fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5 },
  group: { display: 'flex', flexDirection: 'column', gap: 6 },
  groupLabel: {
    fontSize: 10, fontWeight: 700, fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.08em',
  },
  row: {
    display: 'flex', flexDirection: 'column', gap: 4, padding: '8px 10px',
    border: '1px solid var(--border-light)', borderRadius: 4, background: 'var(--bg-primary)',
    textAlign: 'left',
  },
  rowButton: {
    display: 'flex', flexDirection: 'column', gap: 4, padding: '8px 10px',
    border: '1px solid var(--border-light)', borderRadius: 4, background: 'var(--bg-primary)',
    textAlign: 'left', cursor: 'pointer', font: 'inherit', color: 'inherit', width: '100%',
  },
  message: { fontSize: 13, color: 'var(--text-primary)', lineHeight: 1.5 },
  meta: { display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' },
  chapter: {
    fontSize: 10, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)',
    textTransform: 'uppercase', letterSpacing: '0.06em',
  },
  fix: { fontSize: 11, color: 'var(--accent)' },
  empty: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' },
  muted: { fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  error: { fontSize: 12, color: 'var(--danger, #d33)' },
}

/**
 * App health drawer: the same cohesion heuristics the agent's review_dashboard
 * tool runs, as a quiet editor checklist. Findings group by severity; a
 * finding scoped to a widget jumps to (and flashes) that widget in the grid.
 */
export function AppHealthPanel({ dashboardId, onJumpToChapter, onClose }: {
  dashboardId: string
  /** Asks the editor to show a chapter (the widget may live in an inactive one). */
  onJumpToChapter?: (chapterId: string) => void
  onClose: () => void
}) {
  const panelRef = useRef<HTMLDivElement>(null)
  useEscapeToClose(onClose)
  useFocusTrap(panelRef, true)

  // Mounted only while open, and each open is an explicit "check now": the
  // app-wide 30s staleTime must not serve a stale report when the author
  // reopens the panel after fixing something.
  const { data, isLoading, error } = useQuery<ReviewReport>({
    queryKey: ['dashboard-review', dashboardId],
    queryFn: () => api.get<ReviewReport>(`/api/v1/dashboards/${dashboardId}/review`),
    staleTime: 0,
    refetchOnMount: 'always',
  })

  const jumpToWidget = (finding: ReviewFinding) => {
    if (!finding.widget_id) return
    const attempt = (): boolean => {
      const el = document.getElementById(`dash-widget-${finding.widget_id}`)
      if (!el) return false
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      el.focus({ preventScroll: true })
      el.classList.add(WIDGET_HIGHLIGHT_CLASS)
      window.setTimeout(() => el.classList.remove(WIDGET_HIGHLIGHT_CLASS), HIGHLIGHT_MS)
      return true
    }
    if (attempt()) return
    // The widget is not mounted: it lives in an inactive chapter (the editor
    // renders one chapter at a time). Ask for the chapter, then retry once the
    // grid has re-rendered.
    if (finding.chapter_id && onJumpToChapter) {
      onJumpToChapter(finding.chapter_id)
      window.setTimeout(attempt, CHAPTER_SWITCH_MS)
    }
  }

  const findings = data?.findings ?? []

  return createPortal(
    <>
      <div data-testid="app-health-backdrop" style={styles.backdrop} onClick={onClose} aria-hidden="true" />
      <div ref={panelRef} style={styles.panel} role="dialog" aria-modal="true" aria-label="App health" tabIndex={-1}>
        <div style={styles.header}>
          <span style={styles.title}>App health</span>
          <button type="button" style={styles.close} onClick={onClose} aria-label="Close app health">
            <X size={15} />
          </button>
        </div>
        <div style={styles.body}>
          <span style={styles.note}>
            The same composition checks the agent runs before publishing. Findings with a widget jump to it.
          </span>

          {error ? (
            <div style={styles.error} role="alert">{(error as Error).message}</div>
          ) : isLoading ? (
            <span style={styles.muted}>Checking the app…</span>
          ) : findings.length === 0 ? (
            <>
              <span style={styles.empty}>No issues found</span>
              <span style={styles.note}>Layout, content, and surface checks are clear.</span>
            </>
          ) : (
            SEVERITY_GROUPS.map(({ severity, label }) => {
              const group = findings.filter((finding) => finding.severity === severity)
              if (group.length === 0) return null
              return (
                <div key={severity} style={styles.group}>
                  <span style={styles.groupLabel}>{label} ({group.length})</span>
                  {group.map((finding, index) => {
                    const hint = finding.fix_tool ? FIX_HINTS[finding.fix_tool] : undefined
                    const content = (
                      <>
                        <span style={styles.message}>{finding.message}</span>
                        <span style={styles.meta}>
                          {finding.chapter_id && <span style={styles.chapter}>{finding.chapter_id}</span>}
                          {hint && <span style={styles.fix}>{hint}</span>}
                        </span>
                      </>
                    )
                    return finding.widget_id ? (
                      <button
                        key={`${severity}-${index}`}
                        type="button"
                        style={styles.rowButton}
                        onClick={() => jumpToWidget(finding)}
                      >
                        {content}
                      </button>
                    ) : (
                      <div key={`${severity}-${index}`} style={styles.row}>
                        {content}
                      </div>
                    )
                  })}
                </div>
              )
            })
          )}
        </div>
      </div>
    </>,
    document.body,
  )
}
