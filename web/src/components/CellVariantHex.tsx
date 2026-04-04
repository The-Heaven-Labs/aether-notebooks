import { useState } from 'react'
import { Play, Loader2, ChevronUp, ChevronDown, Eye, EyeOff, ChevronRight, Clock, X, Database } from 'lucide-react'
import type { Cell, Connector } from '../types'

// ── Types ─────────────────────────────────────────────────────────────────────

interface SaveState {
  saving: boolean
  savedAt: Date | null
  error: string | null
}

interface Props {
  cell: Cell
  connectors?: Connector[]
  running?: boolean
  saveState?: SaveState
  runAt?: Date
  sourceVisible?: boolean
  onRun?: () => void
  onDelete?: () => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onSwitchType?: () => void
  onToggleSourceVisible?: (v: boolean) => void
  onToggleCellCollapsed?: (v: boolean) => void
  onShowHistory?: () => void
  onUpdateCellMeta?: (updates: Partial<Pick<Cell, 'title' | 'description' | 'slug'>>) => void
  /** For Storybook: render a static code block instead of a live editor */
  staticSource?: string
  /** For Storybook: render static output rows */
  staticOutput?: { columns: string[]; rows: string[][] }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function fmtTime(date: Date): string {
  const diffSec = Math.floor((Date.now() - date.getTime()) / 1000)
  if (diffSec < 5) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// ── Component ─────────────────────────────────────────────────────────────────

export function CellVariantHex({
  cell,
  connectors,
  running = false,
  saveState,
  runAt,
  sourceVisible = true,
  onRun,
  onDelete,
  onMoveUp,
  onMoveDown,
  onSwitchType,
  onToggleSourceVisible,
  onToggleCellCollapsed,
  onShowHistory,
  onUpdateCellMeta,
  staticSource,
  staticOutput,
}: Props) {
  const [hovered, setHovered] = useState(false)

  const isCode = cell.type === 'code'
  const railColor = isCode ? 'var(--accent)' : '#6b9dd8'
  const connector = connectors?.find((c) => c.id === cell.connector_id)

  // ── Collapsed state ────────────────────────────────────────────────────────

  if (cell.cell_collapsed) {
    return (
      <div style={styles.collapsed}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ ...styles.collapsedRail, background: railColor }} />
          <span style={styles.collapsedLabel}>{cell.title || (isCode ? 'SQL cell' : 'Markdown cell')}</span>
        </div>
        <button
          style={styles.expandBtn}
          onClick={() => onToggleCellCollapsed?.(false)}
        >
          <ChevronRight size={12} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 3 }} />
          Expand
        </button>
      </div>
    )
  }

  // ── Normal state ───────────────────────────────────────────────────────────

  return (
    <div
      style={{
        ...styles.cell,
        borderColor: hovered ? 'var(--border)' : 'transparent',
        boxShadow: hovered ? 'var(--shadow-md)' : 'var(--shadow-sm)',
      }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {/* ── Left rail + run button ── */}
      <div style={{ ...styles.rail, background: railColor }}>
        {isCode && (
          <button
            style={{
              ...styles.runCircle,
              opacity: hovered ? 1 : 0,
              background: running ? 'var(--text-muted)' : railColor,
            }}
            onClick={onRun}
            disabled={running}
            title="Run cell (Ctrl+Enter)"
          >
            {running
              ? <Loader2 size={12} style={styles.spin} />
              : <Play size={12} style={{ marginLeft: 1 }} />
            }
          </button>
        )}
      </div>

      {/* ── Body ── */}
      <div style={styles.body}>

        {/* Header row: connector badge + title */}
        <div style={styles.header}>
          <div style={styles.headerLeft}>
            {connector && (
              <span style={styles.connectorBadge}>
                <Database size={10} style={{ flexShrink: 0 }} />
                {connector.name}
              </span>
            )}
            {!connector && isCode && (
              <span style={{ ...styles.connectorBadge, opacity: 0.45 }}>
                <Database size={10} style={{ flexShrink: 0 }} />
                No connector
              </span>
            )}
            {cell.title !== undefined && (
              <input
                style={styles.titleInput}
                value={cell.title}
                onChange={(e) => onUpdateCellMeta?.({ title: e.target.value })}
                placeholder="Untitled cell"
              />
            )}
          </div>

          {/* Hover toolbar */}
          <div style={{ ...styles.toolbar, opacity: hovered ? 1 : 0 }}>
            {isCode && (
              <button style={styles.typeChip} onClick={onSwitchType} title="Switch to markdown">
                SQL <span style={styles.switchHint}>⇄</span>
              </button>
            )}
            {!isCode && (
              <button style={styles.typeChip} onClick={onSwitchType} title="Switch to SQL">
                MD <span style={styles.switchHint}>⇄</span>
              </button>
            )}
            {onMoveUp && (
              <button style={styles.iconBtn} onClick={onMoveUp} title="Move up">
                <ChevronUp size={12} />
              </button>
            )}
            {onMoveDown && (
              <button style={styles.iconBtn} onClick={onMoveDown} title="Move down">
                <ChevronDown size={12} />
              </button>
            )}
            <button
              style={styles.iconBtn}
              onClick={() => onToggleSourceVisible?.(!sourceVisible)}
              title={sourceVisible ? 'Hide source' : 'Show source'}
            >
              {sourceVisible ? <EyeOff size={12} /> : <Eye size={12} />}
            </button>
            <button
              style={styles.iconBtn}
              onClick={() => onToggleCellCollapsed?.(true)}
              title="Collapse cell"
            >
              <ChevronDown size={12} />
            </button>
            <button style={styles.iconBtn} onClick={onShowHistory} title="Cell history">
              <Clock size={12} />
            </button>
            <button style={styles.deleteBtn} onClick={onDelete} title="Delete cell">
              <X size={12} />
            </button>
          </div>
        </div>

        {/* Editor / source */}
        {sourceVisible && staticSource && (
          <pre style={styles.staticEditor}><code>{staticSource}</code></pre>
        )}

        {/* Static output table (for Storybook) */}
        {staticOutput && (
          <div style={styles.outputArea}>
            <table style={styles.table}>
              <thead>
                <tr>
                  {staticOutput.columns.map((col) => (
                    <th key={col} style={styles.th}>{col}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {staticOutput.rows.map((row, i) => (
                  <tr key={i} style={i % 2 === 0 ? {} : { background: 'var(--bg-secondary)' }}>
                    {row.map((cell, j) => (
                      <td key={j} style={styles.td}>{cell}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
            <div style={styles.rowCount}>{staticOutput.rows.length} rows</div>
          </div>
        )}

        {/* Status bar */}
        <div style={styles.statusBar}>
          <span style={saveState?.error ? styles.statusError : styles.statusMuted}>
            {saveState?.saving
              ? 'Saving…'
              : saveState?.error
                ? `Error: ${saveState.error}`
                : saveState?.savedAt
                  ? `Saved ${fmtTime(saveState.savedAt)}`
                  : ''}
          </span>
          {runAt && (
            <span style={styles.statusMuted}>Last run: {runAt.toLocaleTimeString()}</span>
          )}
        </div>
      </div>
    </div>
  )
}

// ── Styles ────────────────────────────────────────────────────────────────────

const styles: Record<string, React.CSSProperties> = {
  cell: {
    position: 'relative',
    display: 'flex',
    borderRadius: 'var(--radius-md)',
    background: 'white',
    border: '1px solid transparent',
    transition: 'border-color 0.15s ease, box-shadow 0.15s ease',
    overflow: 'hidden',
  },

  // Left rail
  rail: {
    width: 4,
    flexShrink: 0,
    position: 'relative',
    display: 'flex',
    justifyContent: 'center',
  },
  runCircle: {
    position: 'absolute',
    top: 10,
    left: -14,
    width: 28,
    height: 28,
    borderRadius: '50%',
    color: 'white',
    border: 'none',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    transition: 'opacity 0.15s ease, background 0.15s ease',
    boxShadow: 'var(--shadow-md)',
    zIndex: 10,
  },
  spin: {
    animation: 'spin 1s linear infinite',
  },

  // Body
  body: {
    flex: 1,
    minWidth: 0,
  },

  // Header
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px 10px 6px 12px',
    borderBottom: '1px solid var(--border-light)',
    minHeight: 36,
    gap: 8,
  },
  headerLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    flex: 1,
    minWidth: 0,
  },
  connectorBadge: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    fontWeight: 600,
    color: 'var(--text-secondary)',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border-light)',
    borderRadius: 20,
    padding: '2px 8px 2px 6px',
    whiteSpace: 'nowrap',
    flexShrink: 0,
  },
  titleInput: {
    flex: 1,
    border: 'none',
    outline: 'none',
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
    background: 'transparent',
    fontFamily: 'var(--font-sans)',
    minWidth: 0,
  },

  // Hover toolbar
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    gap: 2,
    transition: 'opacity 0.15s ease',
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm)',
    padding: '2px 3px',
    boxShadow: 'var(--shadow-sm)',
    flexShrink: 0,
  },
  typeChip: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 3,
    fontSize: 9,
    fontWeight: 700,
    letterSpacing: '0.08em',
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    background: 'var(--border)',
    border: '1px solid transparent',
    borderRadius: 3,
    padding: '1px 5px',
    cursor: 'pointer',
  },
  switchHint: {
    opacity: 0.5,
    fontFamily: 'var(--font-sans)',
    fontSize: 11,
  },
  iconBtn: {
    padding: '2px 5px',
    background: 'transparent',
    border: 'none',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    lineHeight: 1,
    display: 'flex',
    alignItems: 'center',
  },
  deleteBtn: {
    padding: '2px 5px',
    background: 'transparent',
    border: 'none',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-muted)',
    lineHeight: 1,
    display: 'flex',
    alignItems: 'center',
    marginLeft: 1,
  },

  // Static editor (Storybook mock)
  staticEditor: {
    margin: 0,
    padding: '14px 16px',
    fontFamily: 'var(--font-mono)',
    fontSize: 13,
    lineHeight: 1.65,
    color: 'var(--text-primary)',
    background: '#fdfcfb',
    borderBottom: '1px solid var(--border-light)',
    overflowX: 'auto',
    whiteSpace: 'pre',
  },

  // Output table
  outputArea: {
    borderBottom: '1px solid var(--border-light)',
    overflowX: 'auto',
  },
  table: {
    width: '100%',
    borderCollapse: 'collapse',
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
  },
  th: {
    textAlign: 'left',
    padding: '6px 12px',
    fontWeight: 600,
    fontSize: 11,
    color: 'var(--text-secondary)',
    background: 'var(--bg-secondary)',
    borderBottom: '1px solid var(--border-light)',
    whiteSpace: 'nowrap',
  },
  td: {
    padding: '5px 12px',
    color: 'var(--text-primary)',
    borderBottom: '1px solid var(--border-light)',
    whiteSpace: 'nowrap',
  },
  rowCount: {
    padding: '4px 12px',
    fontSize: 11,
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
    background: 'var(--bg-secondary)',
  },

  // Status bar
  statusBar: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: '3px 16px',
    fontSize: 11,
    minHeight: 22,
    background: '#faf9f7',
  },
  statusMuted: {
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
  },
  statusError: {
    color: 'var(--error)',
    fontFamily: 'var(--font-mono)',
  },

  // Collapsed
  collapsed: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    borderRadius: 'var(--radius-md)',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border-light)',
    padding: '7px 14px 7px 0',
    boxShadow: 'var(--shadow-sm)',
    overflow: 'hidden',
  },
  collapsedRail: {
    width: 4,
    alignSelf: 'stretch',
    borderRadius: '0 2px 2px 0',
    flexShrink: 0,
    marginRight: 10,
  },
  collapsedLabel: {
    fontSize: 13,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
  expandBtn: {
    fontSize: 12,
    background: 'transparent',
    border: 'none',
    color: 'var(--accent)',
    cursor: 'pointer',
  },
}
