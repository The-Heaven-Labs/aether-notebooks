import { useState } from 'react'
import { Play, Loader2, ChevronUp, ChevronDown, Eye, EyeOff, Clock, X, ChevronRight } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Cell, Connector } from '../types'

function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^\w-]/g, '')
}

const markdownComponents = {
  h1: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h1 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h1>
    )
  },
  h2: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h2 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h2>
    )
  },
  h3: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h3 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h3>
    )
  },
  h4: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h4 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h4>
    )
  },
  h5: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h5 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h5>
    )
  },
  h6: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h6 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h6>
    )
  },
}

// ── SQL syntax highlighter ────────────────────────────────────────────────────

const SQL_KEYWORDS = /\b(select|from|where|join|left|right|inner|outer|on|group\s+by|order\s+by|having|limit|offset|as|and|or|not|in|like|between|is|null|distinct|union|all|case|when|then|else|end|insert|into|values|update|set|delete|create|table|index|drop|alter|add|column|primary|key|foreign|references|with|count|sum|avg|min|max|coalesce|cast|date_trunc|interval|now|true|false)\b/gi
const SQL_STRINGS = /(["'])(?:(?!\1)[^\\]|\\.)*?\1/g
const SQL_COMMENTS = /(--[^\n]*|\/\*[\s\S]*?\*\/)/g
const SQL_NUMBERS = /\b(\d+(?:\.\d+)?)\b/g
const SQL_FUNCTIONS = /\b([a-z_][a-z0-9_]*)\s*(?=\()/gi

function SqlHighlight({ code }: { code: string }) {
  // Tokenise in one pass — order matters (comments > strings > keywords > functions > numbers)
  type Token = { type: 'kw' | 'str' | 'cmt' | 'fn' | 'num' | 'plain'; text: string }
  const tokens: Token[] = []

  // Build a combined regex that captures all token types in priority order
  const combined = new RegExp(
    [SQL_COMMENTS, SQL_STRINGS, SQL_KEYWORDS, SQL_FUNCTIONS, SQL_NUMBERS]
      .map((r) => r.source)
      .join('|'),
    'gi',
  )

  let last = 0
  let match: RegExpExecArray | null
  combined.lastIndex = 0

  while ((match = combined.exec(code)) !== null) {
    if (match.index > last) tokens.push({ type: 'plain', text: code.slice(last, match.index) })
    const text = match[0]
    if (SQL_COMMENTS.test(text)) tokens.push({ type: 'cmt', text })
    else if (SQL_STRINGS.test(text)) tokens.push({ type: 'str', text })
    else if (SQL_KEYWORDS.test(text)) tokens.push({ type: 'kw', text })
    else if (SQL_FUNCTIONS.test(text)) tokens.push({ type: 'fn', text })
    else if (SQL_NUMBERS.test(text)) tokens.push({ type: 'num', text })
    else tokens.push({ type: 'plain', text })
    // Reset stateful regexes after each test
    SQL_COMMENTS.lastIndex = 0
    SQL_STRINGS.lastIndex = 0
    SQL_KEYWORDS.lastIndex = 0
    SQL_FUNCTIONS.lastIndex = 0
    SQL_NUMBERS.lastIndex = 0
    last = match.index + text.length
  }
  if (last < code.length) tokens.push({ type: 'plain', text: code.slice(last) })

  const colorMap: Record<Token['type'], string | undefined> = {
    kw: 'var(--code-keyword)',
    str: 'var(--code-string)',
    cmt: 'var(--code-comment)',
    fn: 'var(--code-function)',
    num: 'var(--code-number)',
    plain: undefined,
  }

  return (
    <pre style={styles.sourceBlock}>
      <code style={styles.sourceCode}>
        {tokens.map((t, i) =>
          t.type === 'plain'
            ? t.text
            : <span key={i} style={{ color: colorMap[t.type] }}>{t.text}</span>
        )}
      </code>
    </pre>
  )
}

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
  /** Storybook: static source code string */
  staticSource?: string
  /** Storybook: static output table */
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

export function CellVariantWireframe({
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
  const connector = connectors?.find((c) => c.id === cell.connector_id)

  // ── Collapsed ─────────────────────────────────────────────────────────────

  if (cell.cell_collapsed) {
    return (
      <div style={styles.collapsed}>
        <button style={styles.expandTrigger} onClick={() => onToggleCellCollapsed?.(false)}>
          <ChevronRight size={11} />
          <span style={styles.cellTypeTag}>{isCode ? 'SQL' : 'MD'}</span>
          <span style={styles.collapsedTitle}>{cell.title || (isCode ? 'Untitled query' : 'Untitled note')}</span>
        </button>
      </div>
    )
  }

  // ── Normal ────────────────────────────────────────────────────────────────

  return (
    <div
      style={styles.cell}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {/* ── Meta bar: type tag + title + hover actions ── */}
      <div style={styles.metaBar}>
        <div style={styles.metaLeft}>
          <span style={styles.cellTypeTag}>{isCode ? 'SQL' : 'MD'}</span>
          {connector && (
            <span style={styles.connectorLabel}>{connector.name}</span>
          )}
          {cell.title !== undefined && (
            <input
              style={styles.titleInput}
              value={cell.title}
              onChange={(e) => onUpdateCellMeta?.({ title: e.target.value })}
              placeholder="Untitled"
            />
          )}
        </div>

        {/* Hover actions — plain text links, no buttons */}
        <div style={{ ...styles.actions, opacity: hovered ? 1 : 0 }}>
          {isCode && (
            <button style={styles.actionBtn} onClick={onRun} disabled={running} title="Run">
              {running
                ? <Loader2 size={11} style={{ animation: 'spin 1s linear infinite' }} />
                : <Play size={11} />
              }
            </button>
          )}
          <button style={styles.actionBtn} onClick={onSwitchType} title={isCode ? 'Switch to MD' : 'Switch to SQL'}>
            {isCode ? 'MD' : 'SQL'}
          </button>
          {onMoveUp && <button style={styles.actionBtn} onClick={onMoveUp} aria-label="Move up"><ChevronUp size={11} /></button>}
          {onMoveDown && <button style={styles.actionBtn} onClick={onMoveDown} aria-label="Move down"><ChevronDown size={11} /></button>}
          <button style={styles.actionBtn} onClick={() => onToggleSourceVisible?.(!sourceVisible)} aria-label={sourceVisible ? 'Hide source' : 'Show source'}>
            {sourceVisible ? <EyeOff size={11} /> : <Eye size={11} />}
          </button>
          <button style={styles.actionBtn} onClick={() => onToggleCellCollapsed?.(true)} aria-label="Collapse cell"><ChevronRight size={11} /></button>
          <button style={styles.actionBtn} onClick={onShowHistory} aria-label="Cell history"><Clock size={11} /></button>
          <button style={{ ...styles.actionBtn, ...styles.actionBtnDelete }} onClick={onDelete} aria-label="Delete cell"><X size={11} /></button>
        </div>
      </div>

      {/* ── Source block ── */}
      {sourceVisible && staticSource && (
        isCode
          ? <SqlHighlight code={staticSource} />
          : <div style={styles.mdBlock}><ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{staticSource}</ReactMarkdown></div>
      )}

      {/* ── Output table ── */}
      {staticOutput && (
        <div style={styles.outputWrap} tabIndex={0}>
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
                <tr key={i}>
                  {row.map((val, j) => (
                    <td key={j} style={styles.td}>{val}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
          <div style={styles.rowMeta}>{staticOutput.rows.length} rows</div>
        </div>
      )}

      {/* ── Footer: save state + run time ── */}
      {(saveState || runAt) && (
        <div style={styles.footer}>
          <span style={saveState?.error ? styles.footerError : styles.footerMuted}>
            {saveState?.saving
              ? 'Saving…'
              : saveState?.error
                ? saveState.error
                : saveState?.savedAt
                  ? `Saved ${fmtTime(saveState.savedAt)}`
                  : ''}
          </span>
          {runAt && <span style={styles.footerMuted}>Ran {fmtTime(runAt)}</span>}
        </div>
      )}
    </div>
  )
}

// ── Styles ────────────────────────────────────────────────────────────────────

const styles: Record<string, React.CSSProperties> = {
  cell: {
    background: 'var(--bg-card)',
    borderBottom: '1px solid var(--border-light)',
  },

  // Meta bar
  metaBar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '6px 16px',
    gap: 8,
    minHeight: 32,
  },
  metaLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    flex: 1,
    minWidth: 0,
  },
  cellTypeTag: {
    fontSize: 9,
    fontFamily: 'var(--font-mono)',
    fontWeight: 700,
    letterSpacing: '0.1em',
    color: 'var(--text-secondary)',
    textTransform: 'uppercase' as const,
    flexShrink: 0,
  },
  connectorLabel: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
    flexShrink: 0,
  },
  titleInput: {
    flex: 1,
    border: 'none',
    outline: 'none',
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-primary)',
    background: 'transparent',
    fontFamily: 'var(--font-sans)',
    minWidth: 0,
  },

  // Actions strip (hover-only)
  actions: {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    transition: 'opacity 0.12s',
    flexShrink: 0,
  },
  actionBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '3px 6px',
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    fontWeight: 600,
    color: 'var(--text-secondary)',
    background: 'none',
    border: 'none',
    borderRadius: 3,
    cursor: 'pointer',
    lineHeight: 1,
    transition: 'color 0.1s, background 0.1s',
  },
  actionBtnDelete: {
    color: 'var(--text-secondary)',
  },

  // Source block
  sourceBlock: {
    margin: 0,
    padding: '12px 16px',
    background: 'var(--bg-cell-code)',
    borderTop: '1px solid var(--border-light)',
    borderBottom: '1px solid var(--border-light)',
    overflowX: 'auto',
  },
  sourceCode: {
    fontFamily: 'var(--font-mono)',
    fontSize: 12.5,
    lineHeight: 1.7,
    color: 'var(--text-primary)',
    whiteSpace: 'pre',
  },
  mdBlock: {
    padding: '14px 20px',
    fontSize: 14,
    lineHeight: 1.75,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
    borderTop: '1px solid var(--border-light)',
    borderBottom: '1px solid var(--border-light)',
  },
  headerAnchor: {
    color: 'var(--text-muted)',
    textDecoration: 'none',
    marginRight: 8,
    opacity: 0,
    transition: 'opacity 0.15s',
    fontFamily: 'var(--font-mono)',
    fontSize: '0.85em',
  },

  // Output
  outputWrap: {
    borderTop: '1px solid var(--border-light)',
    overflowX: 'auto',
  },
  table: {
    width: '100%',
    borderCollapse: 'collapse',
    fontSize: 12,
  },
  th: {
    textAlign: 'left',
    padding: '5px 16px',
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    borderBottom: '1px solid var(--border-light)',
    background: 'var(--bg-secondary)',
    whiteSpace: 'nowrap',
  },
  td: {
    padding: '4px 16px',
    fontFamily: 'var(--font-mono)',
    fontSize: 12,
    color: 'var(--text-primary)',
    borderBottom: '1px solid var(--border-light)',
    whiteSpace: 'nowrap',
  },
  rowMeta: {
    padding: '3px 16px',
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
    borderTop: '1px solid var(--border-light)',
  },

  // Footer
  footer: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: '3px 16px',
    minHeight: 20,
  },
  footerMuted: {
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
  },
  footerError: {
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    color: 'var(--error-full)',
  },

  // Collapsed
  collapsed: {
    background: 'var(--bg-card)',
    borderBottom: '1px solid var(--border-light)',
    padding: '4px 16px',
  },
  expandTrigger: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    padding: '2px 0',
    color: 'var(--text-secondary)',
  },
  collapsedTitle: {
    fontSize: 12,
    fontFamily: 'var(--font-sans)',
    color: 'var(--text-secondary)',
    fontStyle: 'italic',
  },
}
