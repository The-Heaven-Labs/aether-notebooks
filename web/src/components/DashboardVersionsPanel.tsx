import { useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { RotateCcw, X } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { ConfirmDialog } from './ConfirmDialog'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useFocusTrap } from '../hooks/useFocusTrap'
import { formatExecutedAt } from '../utils/formatDateTime'
import { formatRelativeTime } from '../utils/formatRelativeTime'
import type { DashboardVersionsResponse } from '../types'

/**
 * The dashboard's published-version status. Polled while the editor is open so
 * a publish from another tab (or this client's own invalidation) refreshes the
 * header chip. The endpoint requires dashboard `edit`, so callers must keep
 * `enabled` false until the dashboard response has confirmed access: enabling
 * it while the dashboard is still loading would fire a 403 for viewers.
 */
export function useDashboardVersions(dashboardId: string | undefined, enabled = true) {
  return useQuery<DashboardVersionsResponse>({
    queryKey: ['dashboard-versions', dashboardId],
    queryFn: () => api.get<DashboardVersionsResponse>(`/api/v1/dashboards/${dashboardId}/versions`),
    enabled: !!dashboardId && enabled,
    refetchInterval: 8000,
  })
}

/**
 * Version-history drawer for the dashboard editor: the published versions
 * (newest first, with publisher and time), a restore action per version, and
 * the draft warnings that block publishing.
 *
 * Restore copies a snapshot back into the draft and never publishes: the live
 * app keeps rendering the last published version until the author publishes
 * explicitly, which is why the confirmation says so. Restoring is destructive
 * to unpublished draft edits, so it goes through ConfirmDialog.
 *
 * While the confirmation is up it owns Escape and focus; the drawer hands both
 * over (its own Escape handler and focus trap are disabled) and takes them back
 * when the dialog closes.
 */
export function DashboardVersionsPanel({ dashboardId, onClose }: {
  dashboardId: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const { data, isLoading, error } = useDashboardVersions(dashboardId)
  const [confirmVersion, setConfirmVersion] = useState<number | null>(null)
  const [restoredVersion, setRestoredVersion] = useState<number | null>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  // The row button that opened the confirmation; the focus trap re-focuses it
  // when the dialog closes (the dialog's Cancel button is gone by then).
  const lastRestoreRef = useRef<HTMLButtonElement | null>(null)

  const restore = useMutation({
    mutationFn: (version: number) =>
      api.post<{ restored: number }>(`/api/v1/dashboards/${dashboardId}/versions/${version}/restore`),
    onSuccess: (_result, version) => {
      setRestoredVersion(version)
      qc.invalidateQueries({ queryKey: ['dashboard-versions', dashboardId] })
      qc.invalidateQueries({ queryKey: ['dashboard', dashboardId] })
    },
  })

  const confirmOpen = confirmVersion !== null
  useEscapeToClose(onClose, !confirmOpen)
  // The initial focus target is the row's Restore button so closing the
  // confirmation lands back on it; on mount (no button yet) it falls back to
  // the panel itself.
  useFocusTrap(panelRef, !confirmOpen, lastRestoreRef)

  const versions = data?.versions ?? []
  const latest = data?.latest ?? null
  const warnings = data?.warnings ?? []

  return createPortal(
    <>
      <div data-testid="dashboard-versions-backdrop" style={styles.backdrop} onClick={onClose} aria-hidden="true" />
      <div ref={panelRef} style={styles.panel} role="dialog" aria-modal="true" aria-label="Version history" tabIndex={-1}>
        <div style={styles.header}>
          <span style={styles.title}>Version history</span>
          <button type="button" style={styles.close} onClick={onClose} aria-label="Close version history">
            <X size={15} />
          </button>
        </div>
        <div style={styles.body}>
          <span style={styles.note}>Restoring copies this version into the draft — publish to make it live.</span>

          {warnings.length > 0 && (
            <div style={styles.warningBox}>
              <strong>This draft cannot be published yet.</strong>
              <ul style={styles.warningList}>
                {warnings.map((warning, index) => <li key={`${index}-${warning}`}>{warning}</li>)}
              </ul>
            </div>
          )}

          {restore.isPending && <span style={styles.muted}>Restoring v{restore.variables}…</span>}
          {restoredVersion !== null && !restore.isPending && !restore.isError && (
            <span style={styles.success}>Restored v{restoredVersion} into the draft.</span>
          )}
          {restore.isError && <div style={styles.error} role="alert">{(restore.error as Error).message}</div>}
          {error && <div style={styles.error} role="alert">{(error as Error).message}</div>}

          {isLoading ? (
            <span style={styles.muted}>Loading…</span>
          ) : versions.length === 0 && !error ? (
            // The error branch above renders its own message; never stack the
            // empty state on top of it.
            <span style={styles.muted}>No versions published yet — publish the draft to create v1.</span>
          ) : (
            <div style={styles.list}>
              {versions.map((v) => (
                <div key={v.version} data-testid="version-row" style={styles.row}>
                  <div style={styles.rowMain}>
                    <span style={styles.versionLabel}>v{v.version}</span>
                    {latest?.version === v.version && <span style={styles.publishedBadge}>Published</span>}
                    <span style={styles.rowMeta}>
                      <span>{v.created_by_email}</span>
                      {' · '}
                      <span title={formatExecutedAt(v.created_at)}>{formatRelativeTime(v.created_at)}</span>
                    </span>
                  </div>
                  <button
                    type="button"
                    style={styles.restoreBtn}
                    aria-label={`Restore v${v.version}`}
                    disabled={restore.isPending}
                    onClick={(e) => {
                      lastRestoreRef.current = e.currentTarget
                      setConfirmVersion(v.version)
                    }}
                  >
                    <RotateCcw size={11} /> Restore
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
      {confirmOpen && (
        <div style={styles.confirmLayer}>
          <ConfirmDialog
            open
            title={`Restore version v${confirmVersion}?`}
            message="This copies the version into the draft. Unpublished draft changes are replaced; the live app keeps showing the last published version until you publish."
            confirmLabel="Restore"
            destructive
            onConfirm={() => {
              restore.mutate(confirmVersion)
              setConfirmVersion(null)
            }}
            onCancel={() => setConfirmVersion(null)}
          />
        </div>
      )}
    </>,
    document.body,
  )
}

const styles: Record<string, React.CSSProperties> = {
  // Matches the App settings / widget drawers: the panel covers the top bar
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
  body: { padding: 16, display: 'flex', flexDirection: 'column', gap: 10, overflowY: 'auto', flex: 1 },
  note: { fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5 },
  warningBox: {
    border: '1px solid var(--warning-border)', background: 'var(--warning-light)',
    color: 'var(--warning-text)', borderRadius: 4, padding: '8px 10px', fontSize: 12, lineHeight: 1.5,
  },
  warningList: { margin: '4px 0 0', paddingLeft: 18 },
  muted: { fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  success: { fontSize: 12, color: 'var(--success)' },
  error: { fontSize: 12, color: 'var(--danger, #d33)' },
  list: { display: 'flex', flexDirection: 'column', gap: 8 },
  row: {
    display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8,
    border: '1px solid var(--border)', borderRadius: 4, padding: '8px 10px',
  },
  rowMain: { display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flexWrap: 'wrap' },
  versionLabel: { fontSize: 13, fontWeight: 700, color: 'var(--text-primary)', fontFamily: 'var(--font-mono)' },
  publishedBadge: {
    fontSize: 10, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.04em',
    color: 'var(--success)', border: '1px solid var(--success)', borderRadius: 999, padding: '1px 6px',
  },
  rowMeta: { fontSize: 12, color: 'var(--text-muted)' },
  restoreBtn: {
    display: 'inline-flex', alignItems: 'center', gap: 4, flexShrink: 0,
    padding: '4px 10px', fontSize: 12, fontWeight: 600,
    background: 'var(--bg-input)', color: 'var(--text-primary)',
    border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer',
  },
  // The drawer sits at z-index 1701 while ConfirmDialog's overlay is 1000; this
  // layer lifts the confirmation above the drawer without changing the shared
  // dialog. The dialog's own overlay still owns outside clicks.
  confirmLayer: { position: 'fixed', inset: 0, zIndex: 1800 },
}
