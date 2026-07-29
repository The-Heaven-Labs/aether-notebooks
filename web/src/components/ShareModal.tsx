import { useState, useEffect, useRef } from 'react'
import { api } from '../api/client'
import type { Member, Notebook, Output, Parameter } from '../types'
import { ConfirmModal } from './ConfirmModal'

interface ShareModalProps {
  resourceType: 'notebook' | 'dashboard'
  resourceId: string
  canShare?: boolean
  onClose: () => void
  initialTab?: 'link' | 'embed'
  initialCellId?: string
}

interface ShareResponse {
  token: string
  created_by?: string
  created_at?: string
}

interface PublicCell {
  id: string
  position: number
  type: string
  language?: string
  source: string
  outputs: Output[]
  parameters?: Parameter[]
  metadata?: Record<string, unknown>
  outputs_hidden: boolean
}

const CELL_TYPE_LABELS: Record<string, string> = { code: 'code', text: 'text' }

export function ShareModal({ resourceType, resourceId, canShare = true, onClose, initialTab, initialCellId }: ShareModalProps) {
  const [token, setToken] = useState<string | null>(null)
  const [createdBy, setCreatedBy] = useState<string | null>(null)
  const [createdAt, setCreatedAt] = useState<string | null>(null)
  const [creatorName, setCreatorName] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [revoking, setRevoking] = useState(false)
  const [cells, setCells] = useState<PublicCell[] | null>(null)
  const [embedTab, setEmbedTab] = useState(initialTab === 'embed')
  const [copiedEmbedId, setCopiedEmbedId] = useState<string | null>(null)
  const [flashCellId, setFlashCellId] = useState<string | null>(null)
  const [confirmShare, setConfirmShare] = useState(false)
  const initialCellRef = useRef<HTMLDivElement>(null)

  const publicUrl = token
    ? resourceType === 'dashboard'
      ? `${window.location.origin}/public/dashboards/${token}`
      : `${window.location.origin}/public/${token}`
    : null

  // Resolve creator name from members
  useEffect(() => {
    if (!createdBy) { setCreatorName(null); return }
    api.get<Member[]>('/api/v1/members').then(members => {
      const m = members.find(m => m.user_id === createdBy)
      setCreatorName(m ? (m.name || m.email) : createdBy.slice(0, 8))
    }).catch(() => setCreatorName(createdBy.slice(0, 8)))
  }, [createdBy])

  // Fetch notebook cells for embed when token exists (notebooks only)
  useEffect(() => {
    if (!token || resourceType !== 'notebook') return
    (async () => {
      try {
        const res = await api.get<{ type: string; notebook: Notebook; cells: PublicCell[] }>(`/api/v1/public/${token}`)
        setCells(res.cells)
      } catch {
        // ignore fetch errors
      }
    })()
  }, [token, resourceType])

  // Ask for confirmation before auto-creating token (embed tab)
  useEffect(() => {
    if (loading || !canShare || initialTab !== 'embed' || resourceType !== 'notebook' || token) return
    setConfirmShare(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading])

  // Scroll to initial cell and trigger flash when cells are loaded
  useEffect(() => {
    if (!initialCellId || !cells || !initialCellRef.current) return
    initialCellRef.current.scrollIntoView({ behavior: 'smooth', block: 'center' })
    setFlashCellId(initialCellId)
    const timer = setTimeout(() => setFlashCellId(null), 2500)
    return () => clearTimeout(timer)
  }, [cells, initialCellId])

  // Clear flash highlight after delay (for tab switches after initial load)
  useEffect(() => {
    if (!flashCellId) return
    const timer = setTimeout(() => setFlashCellId(null), 2500)
    return () => clearTimeout(timer)
  }, [flashCellId])

  // Check for existing token on mount (GET — never creates)
  useEffect(() => {
    (async () => {
      try {
        const res = await api.get<ShareResponse | undefined>(`/api/v1/${resourceType}s/${resourceId}/share`)
        if (res) {
          setToken(res.token)
          setCreatedBy(res.created_by ?? null)
          setCreatedAt(res.created_at ?? null)
        }
      } catch {
        // No existing token — user will need to generate one
      } finally {
        setLoading(false)
      }
    })()
  }, [resourceType, resourceId])

  async function handleShare() {
    setError(null)
    try {
      const res = await api.post<ShareResponse>(`/api/v1/${resourceType}s/${resourceId}/share`, {})
      setToken(res.token)
      setCreatedBy(res.created_by ?? null)
      setCreatedAt(res.created_at ?? null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function handleRevoke() {
    setRevoking(true)
    setError(null)
    try {
      await api.delete(`/api/v1/${resourceType}s/${resourceId}/share`)
      setToken(null)
      setCreatedBy(null)
      setCreatedAt(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRevoking(false)
    }
  }

  async function handleCopy() {
    if (!publicUrl) return
    try {
      await navigator.clipboard.writeText(publicUrl)
    } catch {
      // Fallback: select the input and execCommand
      const input = document.querySelector<HTMLInputElement>(`[data-public-url]`)
      if (input) { input.select(); document.execCommand('copy') }
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const isNotebook = resourceType === 'notebook'

  async function handleCopyEmbed(cellId: string) {
    if (!token) return
    const embedUrl = `${window.location.origin}/embed/${token}/${cellId}`
    const iframeCode = `<iframe src="${embedUrl}" width="100%" height="400" frameborder="0" style="border:1px solid var(--border)"></iframe>`
    try {
      await navigator.clipboard.writeText(iframeCode)
    } catch {
      const ta = document.createElement('textarea')
      ta.value = iframeCode
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
    }
    setCopiedEmbedId(cellId)
    setTimeout(() => setCopiedEmbedId(null), 2000)
  }

  return (
    <div style={s.backdrop} onClick={onClose}>
      {confirmShare && (
        <ConfirmModal
          title="Public access"
          message={`Creating a public link will make this ${resourceType} accessible to anyone with the link. Are you sure?`}
          confirmLabel="Create public link"
          cancelLabel="Cancel"
          onConfirm={() => { setConfirmShare(false); handleShare() }}
          onCancel={() => setConfirmShare(false)}
          destructive
        />
      )}
      <style>{`
        @keyframes embed-cell-flash {
          0% { background: var(--accent-bg); }
          100% { background: var(--bg-secondary); }
        }
      `}</style>
      <div style={s.modal} onClick={e => e.stopPropagation()}>
        <div style={s.header}>
          <span style={s.title}>Share {resourceType}</span>
          <button style={s.closeBtn} onClick={onClose}>×</button>
        </div>
        <div style={s.body}>
          {loading && <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Checking existing link…</p>}
          {error && <p style={{ color: 'var(--error)', fontSize: 13 }}>{error}</p>}
          {!loading && !token && canShare && (
            <button style={s.btn} onClick={() => setConfirmShare(true)}>Generate public link</button>
          )}
          {!loading && !token && !canShare && (
            <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: 0 }}>No public link has been created for this {resourceType}.</p>
          )}
          {!loading && token && (
            <>
              {/* Tabs */}
              {isNotebook && (
                <div style={s.tabs}>
                  <button
                    style={{ ...s.tab, ...(!embedTab ? s.tabActive : {}) }}
                    onClick={() => setEmbedTab(false)}
                  >
                    Link
                  </button>
                  <button
                    style={{ ...s.tab, ...(embedTab ? s.tabActive : {}) }}
                    onClick={() => setEmbedTab(true)}
                  >
                    Embed
                  </button>
                </div>
              )}
              {!embedTab ? (
                <>
                  <div style={s.urlRow}>
                    <input style={s.urlInput} value={publicUrl!} readOnly data-public-url />
                    <button style={s.copyBtn} onClick={handleCopy}>{copied ? 'Copied!' : 'Copy'}</button>
                  </div>
                  {(creatorName || createdAt) && (
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 8 }}>
                      Created by {creatorName ?? 'Unknown'}{createdAt ? ` · ${new Date(createdAt).toLocaleString()}` : ''}
                    </div>
                  )}
                  {canShare && (
                    <button style={{ ...s.btn, marginTop: 12, background: 'var(--error)', color: '#fff' }} onClick={handleRevoke} disabled={revoking}>
                      {revoking ? 'Revoking…' : 'Revoke public link'}
                    </button>
                  )}
                </>
              ) : (
                <div style={s.embedSection}>
                  {!cells ? (
                    <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading cells…</p>
                  ) : cells.length === 0 ? (
                    <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No cells in this {resourceType}.</p>
                  ) : (
                    <div style={s.cellList}>
                      {cells.map(cell => (
                        <div
                          key={cell.id}
                          ref={cell.id === initialCellId ? initialCellRef : undefined}
                          style={{
                            ...s.cellItem,
                            ...(cell.id === flashCellId ? { animation: 'embed-cell-flash 1s ease-out 0.5s both' } : {}),
                          }}
                        >
                          <div style={s.cellMeta}>
                            <span style={s.cellPosition}>{cell.position + 1}</span>
                            <span style={s.cellType}>{CELL_TYPE_LABELS[cell.type] ?? cell.type}</span>
                            {cell.type === 'code' && (
                              <span style={s.cellSource}>
                                {cell.source.slice(0, 60).replace(/\n/g, ' ')}{cell.source.length > 60 ? '…' : ''}
                              </span>
                            )}
                            {cell.type === 'text' && (
                              <span style={s.cellSource}>Markdown</span>
                            )}
                          </div>
                          <button
                            style={s.copyBtn}
                            onClick={() => handleCopyEmbed(cell.id)}
                          >
                            {copiedEmbedId === cell.id ? 'Copied!' : 'Copy embed'}
                          </button>
                        </div>
                      ))}
                    </div>
                  )}
                  {cells && (
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 12, lineHeight: 1.5 }}>
                      Each embed generates an iframe that displays just the cell output. Use the ?view=chart or ?view=table parameter to force a display mode.
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

const s: Record<string, React.CSSProperties> = {
  backdrop: { position: 'fixed', inset: 0, background: 'var(--bg-overlay)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 2000 },
  modal: { background: 'var(--bg-card)', borderRadius: 8, boxShadow: 'var(--shadow-lg)', width: 480, maxHeight: '80vh', overflow: 'hidden', display: 'flex', flexDirection: 'column' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '14px 18px', borderBottom: '1px solid var(--border)', flexShrink: 0 },
  title: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' },
  closeBtn: { background: 'none', border: 'none', cursor: 'pointer', fontSize: 20, color: 'var(--text-muted)', lineHeight: 1, padding: '0 4px' },
  body: { padding: '18px', overflow: 'auto' },
  btn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  urlRow: { display: 'flex', gap: 8 },
  urlInput: { flex: 1, padding: '7px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, color: 'var(--text-primary)', background: 'var(--bg-input)', fontFamily: 'var(--font-mono)' },
  copyBtn: { padding: '5px 12px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 12, cursor: 'pointer', whiteSpace: 'nowrap' },
  tabs: { display: 'flex', gap: 0, marginBottom: 14, borderBottom: '1px solid var(--border)' },
  tab: { padding: '6px 16px', background: 'none', border: 'none', borderBottom: '2px solid transparent', fontSize: 13, color: 'var(--text-muted)', cursor: 'pointer' },
  tabActive: { color: 'var(--accent)', borderBottomColor: 'var(--accent)', fontWeight: 600 },
  embedSection: { },
  cellList: { display: 'flex', flexDirection: 'column', gap: 6, maxHeight: 300, overflow: 'auto' },
  cellItem: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '8px 12px', background: 'var(--bg-secondary)', borderRadius: 4, gap: 8 },
  cellMeta: { display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flex: 1 },
  cellPosition: { fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, flexShrink: 0 },
  cellType: { fontSize: 10, color: 'var(--accent)', background: 'var(--accent-bg)', padding: '1px 6px', borderRadius: 3, fontWeight: 600, textTransform: 'uppercase', flexShrink: 0 },
  cellSource: { fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', minWidth: 0 },
}
