import { useEffect, useId, useRef, useState } from 'react'
import type React from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  BookOpen, Bot, Cpu, Database, Folder, LayoutDashboard, Plug, Search, Sparkles, Wrench,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { api } from '../api/client'
import { useResourceSearch } from '../hooks/useResourceSearch'
import { useFavorites } from '../hooks/useFavorites'
import { useFocusTrap } from '../hooks/useFocusTrap'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useMediaQuery } from '../hooks/useMediaQuery'
import { HighlightedText } from '../utils/highlight'
import { Kbd } from './Kbd'
import { Skeleton } from './Skeleton'
import type { FavoriteResourceType, RecentItem, ResourceSummary } from '../types'

interface Props {
  open: boolean
  onClose: () => void
  /** Replaces the default `/search?q=` navigation of the "View all results" row. */
  onOpenResults?: (query: string) => void
}

const TYPE_ICONS: Record<FavoriteResourceType, LucideIcon> = {
  folder: Folder,
  notebook: BookOpen,
  dashboard: LayoutDashboard,
  connector: Database,
  agent: Bot,
  model_config: Cpu,
  skill: Sparkles,
  mcp_server: Plug,
  tool: Wrench,
}

/** Group-header labels (plural) for result sections, in the API's group order. */
const TYPE_LABELS: Record<FavoriteResourceType, string> = {
  folder: 'Folders',
  notebook: 'Notebooks',
  dashboard: 'Dashboards',
  connector: 'Connectors',
  agent: 'Agents',
  model_config: 'Models',
  skill: 'Skills',
  mcp_server: 'MCP servers',
  tool: 'Tools',
}

/** Singular labels for the recents rows' meta line. */
const TYPE_SINGULAR: Record<FavoriteResourceType, string> = {
  folder: 'Folder',
  notebook: 'Notebook',
  dashboard: 'Dashboard',
  connector: 'Connector',
  agent: 'Agent',
  model_config: 'Model',
  skill: 'Skill',
  mcp_server: 'MCP server',
  tool: 'Tool',
}

/** The resource types `/recent` can return, mapped onto the catalog union. */
const RECENT_TYPES: Record<string, FavoriteResourceType> = {
  notebook: 'notebook',
  dashboard: 'dashboard',
  connector: 'connector',
}

/** True for the nine catalog types the palette can render. */
function isKnownType(type: string): type is FavoriteResourceType {
  return Object.prototype.hasOwnProperty.call(TYPE_ICONS, type)
}

/** Where Enter (or a click) takes each resource type. */
function resourceHref(type: FavoriteResourceType, id: string): string {
  switch (type) {
    case 'notebook': return `/notebooks/${id}`
    case 'dashboard': return `/dashboards/${id}/view`
    case 'folder': return `/?folder=${id}`
    case 'connector': return '/connectors'
    case 'agent': return '/agents'
    case 'model_config': return '/models'
    case 'skill': return '/skills'
    case 'tool': return '/tools'
    case 'mcp_server': return '/mcps'
  }
}

interface PaletteRow {
  key: string
  icon: LucideIcon
  title: string
  /** Instrument-label meta line: `folder_path · subtitle` (either part optional). */
  meta: string
  snippet?: string
  href: string
  /** The synthetic "View all results" row; activation may call onOpenResults. */
  viewAll?: boolean
}

interface PaletteSection {
  key: string
  label: string
  rows: PaletteRow[]
}

/**
 * Ctrl/Cmd+K command palette: debounced catalog search over a portal dialog
 * with the favorites/recents empty state. Routing-free by contract — Enter
 * targets are resolved here; "View all results" defers to `onOpenResults`
 * when the app wires one.
 */
export function SearchPalette({ open, onClose, onOpenResults }: Props) {
  const navigate = useNavigate()
  const isMobile = useMediaQuery(768)
  const idPrefix = useId()
  const [inputValue, setInputValue] = useState('')
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const dialogRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // 150ms debounce: the input stays instant, the request fires on the settled
  // value (keepPreviousData renders the prior results meanwhile). Closing
  // cancels the pending timer so a palette closed right after a keystroke
  // never fires a request.
  useEffect(() => {
    if (!open) return
    const timer = window.setTimeout(() => {
      setQuery(inputValue.trim())
      setActiveIndex(0)
    }, 150)
    return () => window.clearTimeout(timer)
  }, [inputValue, open])

  // Fresh session on every open; the palette may stay mounted while closed.
  // (Render-time adjustment: the documented alternative to resetting state in
  // an effect, which would flash the previous session's results.)
  const [prevOpen, setPrevOpen] = useState(open)
  if (open !== prevOpen) {
    setPrevOpen(open)
    if (open) {
      setInputValue('')
      setQuery('')
      setActiveIndex(0)
    }
  }

  const search = useResourceSearch({ query })
  const { groups: favoriteGroups, isLoading: favoritesLoading } = useFavorites()
  const { data: recentItems, isLoading: recentLoading } = useQuery<RecentItem[]>({
    queryKey: ['recent'],
    queryFn: () => api.get<RecentItem[]>('/api/v1/recent'),
    enabled: open && query.length === 0,
  })

  useFocusTrap(dialogRef, open, inputRef)
  useEscapeToClose(onClose, open)

  const searching = query.length > 0

  const summaryRow = (result: ResourceSummary): PaletteRow => ({
    key: `${result.resource_type}:${result.id}`,
    icon: TYPE_ICONS[result.resource_type],
    title: result.title,
    meta: [result.folder_path, result.subtitle].filter(Boolean).join(' · '),
    snippet: result.snippet,
    href: resourceHref(result.resource_type, result.id),
  })

  const sections: PaletteSection[] = []
  if (searching) {
    for (const group of search.data?.groups ?? []) {
      // A retired/unknown type would have no icon and crash the render; skip
      // it (and any result row carrying one) instead of showing a broken row.
      if (!isKnownType(group.resource_type)) continue
      const rows = group.results
        .filter((result) => isKnownType(result.resource_type))
        .map(summaryRow)
      if (!rows.length) continue
      sections.push({ key: group.resource_type, label: TYPE_LABELS[group.resource_type], rows })
    }
  } else {
    const favorites = favoriteGroups.flatMap((group) => group.results).slice(0, 5)
    if (favorites.length) {
      sections.push({ key: 'favorites', label: 'Favorites', rows: favorites.map(summaryRow) })
    }
    const recents = (recentItems ?? [])
      .map<PaletteRow | null>((item) => {
        const type = RECENT_TYPES[item.type]
        if (!type) return null
        return {
          key: `recent:${type}:${item.id}`,
          icon: TYPE_ICONS[type],
          title: item.name,
          meta: TYPE_SINGULAR[type],
          href: resourceHref(type, item.id),
        }
      })
      .filter((row): row is PaletteRow => row !== null)
      .slice(0, 5)
    if (recents.length) sections.push({ key: 'recents', label: 'Recents', rows: recents })
  }

  const flatRows = sections.flatMap((section) => section.rows)
  const viewAllRow: PaletteRow | null = searching && flatRows.length > 0
    ? {
        key: 'view-all',
        icon: Search,
        title: 'View all results',
        meta: '',
        href: `/search?q=${encodeURIComponent(query)}`,
        viewAll: true,
      }
    : null
  const rows = viewAllRow ? [...flatRows, viewAllRow] : flatRows
  const active = rows.length ? Math.min(activeIndex, rows.length - 1) : -1

  const rowIndex = new Map<PaletteRow, number>()
  rows.forEach((row, index) => rowIndex.set(row, index))

  const listboxId = `${idPrefix}-listbox`
  const inputId = `${idPrefix}-input`
  const optionId = (index: number) => `${idPrefix}-opt-${index}`

  // Keep the active row in view while arrowing through the list.
  useEffect(() => {
    if (!open || active < 0) return
    const el = dialogRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)
    el?.scrollIntoView?.({ block: 'nearest' })
  }, [open, active])

  if (!open) return null

  const activate = (row: PaletteRow) => {
    onClose()
    if (row.viewAll) {
      if (onOpenResults) onOpenResults(query)
      else navigate(row.href)
      return
    }
    navigate(row.href)
  }

  const onPanelKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Home' || e.key === 'End') {
      if (!rows.length) return
      e.preventDefault()
      const current = active < 0 ? 0 : active
      if (e.key === 'ArrowDown') setActiveIndex((current + 1) % rows.length)
      else if (e.key === 'ArrowUp') setActiveIndex((current - 1 + rows.length) % rows.length)
      else if (e.key === 'Home') setActiveIndex(0)
      else setActiveIndex(rows.length - 1)
      return
    }
    if (e.key === 'Enter') {
      const pending = inputValue.trim()
      if (pending !== query) {
        // Enter arrived before the debounce settled: flush the input so the
        // next Enter acts on the fresh results, never on the stale rows this
        // keystroke invalidated.
        e.preventDefault()
        setQuery(pending)
        setActiveIndex(0)
        return
      }
      const row = rows[active < 0 ? 0 : active]
      if (!row) return
      e.preventDefault()
      activate(row)
    }
  }

  const renderOption = (row: PaletteRow) => {
    const Icon = row.icon
    const index = rowIndex.get(row) ?? 0
    const isActive = index === active
    // The synthetic "View all results" row is about the query, not a match in
    // it: never highlight it against the live terms.
    const highlightQuery = row.viewAll ? '' : query
    return (
      <div
        key={row.key}
        id={optionId(index)}
        role="option"
        aria-selected={isActive}
        data-index={index}
        style={{ ...styles.row, ...(isActive ? styles.rowActive : {}) }}
        onPointerDown={(e) => e.preventDefault()}
        onClick={() => activate(row)}
        onMouseEnter={() => setActiveIndex(index)}
      >
        <Icon size={14} strokeWidth={2} style={styles.rowIcon} aria-hidden="true" />
        <div style={styles.rowBody}>
          <div style={styles.rowTitle}>
            <HighlightedText text={row.title} query={highlightQuery} />
          </div>
          {row.snippet && (
            <div style={styles.rowSnippet}>
              <HighlightedText text={row.snippet} query={highlightQuery} />
            </div>
          )}
          {row.meta && <div style={styles.rowMeta}>{row.meta}</div>}
        </div>
      </div>
    )
  }

  const renderBody = () => {
    if (searching) {
      if (search.isError) {
        return (
          <div style={styles.noticeLine}>
            <span>Search failed</span>
            <button type="button" style={styles.retry} onClick={() => { void search.refetch() }}>
              Retry
            </button>
          </div>
        )
      }
      if (search.isLoading) {
        return (
          <div style={styles.skeletons}>
            <Skeleton count={4} height={14} />
          </div>
        )
      }
      if (!sections.length) {
        return (
          <div style={styles.notice}>
            <div style={styles.noticeTitle}>No matches for “{query}”</div>
            <div style={styles.noticeHint}>Try a different word or a shorter query.</div>
          </div>
        )
      }
      return (
        <>
          {sections.map((section) => (
            <div key={section.key} role="group" aria-label={section.label}>
              <div style={styles.groupHeader}>{section.label}</div>
              {section.rows.map(renderOption)}
            </div>
          ))}
          {viewAllRow && (
            <div role="group" aria-label="All results" style={styles.viewAllGroup}>
              {renderOption(viewAllRow)}
            </div>
          )}
        </>
      )
    }
    return (
      <>
        {sections.map((section) => (
          <div key={section.key} role="group" aria-label={section.label}>
            <div style={styles.groupHeader}>{section.label}</div>
            {section.rows.map(renderOption)}
          </div>
        ))}
        {!sections.length && (favoritesLoading || recentLoading) && (
          <div style={styles.skeletons}>
            <Skeleton count={3} height={14} />
          </div>
        )}
        <div style={styles.hint}>Search titles and content across the catalog.</div>
      </>
    )
  }

  return createPortal(
    <div className="scrim-enter" style={styles.overlay} onClick={onClose}>
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label="Search palette"
        tabIndex={-1}
        style={{ ...styles.panel, marginTop: isMobile ? '8vh' : '15vh' }}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={onPanelKeyDown}
      >
        <div style={styles.inputRow}>
          <Search size={16} style={styles.searchIcon} aria-hidden="true" />
          <input
            ref={inputRef}
            id={inputId}
            type="text"
            role="combobox"
            aria-expanded={rows.length > 0}
            aria-controls={listboxId}
            aria-activedescendant={rows.length && active >= 0 ? optionId(active) : undefined}
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            placeholder="Search files, dashboards, SQL…"
            aria-label="Search"
            aria-autocomplete="list"
            autoComplete="off"
            spellCheck={false}
            style={styles.input}
          />
          <Kbd>Esc</Kbd>
        </div>
        <div
          id={listboxId}
          role={rows.length ? 'listbox' : undefined}
          aria-labelledby={rows.length ? inputId : undefined}
          style={styles.results}
        >
          {renderBody()}
        </div>
        <div style={styles.footer}>
          <span style={styles.footerHint}><Kbd>↑↓</Kbd> navigate</span>
          <span style={styles.footerHint}><Kbd>↵</Kbd> open</span>
        </div>
      </div>
    </div>,
    document.body,
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'fixed',
    inset: 0,
    zIndex: 1700,
    background: 'var(--bg-overlay)',
    display: 'flex',
    alignItems: 'flex-start',
    justifyContent: 'center',
    padding: '0 4vw',
  },
  panel: {
    width: 'min(640px, 92vw)',
    maxHeight: '68vh',
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    boxShadow: 'var(--shadow-lg)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    outline: 'none',
  },
  inputRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '12px 14px',
    borderBottom: '1px solid var(--border-light)',
    flexShrink: 0,
  },
  searchIcon: { color: 'var(--text-muted)', flexShrink: 0 },
  input: {
    flex: 1,
    minWidth: 0,
    background: 'none',
    border: 'none',
    outline: 'none',
    color: 'var(--text-primary)',
    fontSize: 14,
    fontFamily: 'var(--font-sans)',
    padding: 0,
  },
  results: {
    flex: 1,
    minHeight: 0,
    overflowY: 'auto',
    padding: '6px 0',
    outline: 'none',
  },
  groupHeader: {
    padding: '10px 14px 4px',
    fontSize: 10,
    fontWeight: 700,
    letterSpacing: '0.08em',
    textTransform: 'uppercase',
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
  },
  row: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: 10,
    padding: '7px 14px',
    cursor: 'pointer',
    transition: 'background 0.15s ease',
  },
  rowActive: { background: 'color-mix(in srgb, var(--text-primary) 6%, transparent)' },
  rowIcon: { color: 'var(--accent)', flexShrink: 0, marginTop: 2 },
  rowBody: { flex: 1, minWidth: 0 },
  rowTitle: {
    fontSize: 13,
    fontWeight: 500,
    color: 'var(--text-primary)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  rowSnippet: {
    marginTop: 2,
    fontSize: 12,
    // Snippets quote SQL/code: mono, per the Two Scripts Rule.
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  rowMeta: {
    marginTop: 2,
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  viewAllGroup: { borderTop: '1px solid var(--border-light)', marginTop: 6 },
  hint: { padding: '10px 14px 12px', fontSize: 12, color: 'var(--text-muted)' },
  notice: { padding: '20px 14px' },
  noticeTitle: { fontSize: 13, color: 'var(--text-secondary)' },
  noticeHint: { marginTop: 4, fontSize: 12, color: 'var(--text-muted)' },
  noticeLine: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '18px 14px',
    fontSize: 13,
    color: 'var(--text-secondary)',
  },
  retry: {
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontSize: 12,
    padding: '3px 10px',
  },
  skeletons: { padding: '14px', display: 'flex', flexDirection: 'column' },
  footer: {
    display: 'flex',
    alignItems: 'center',
    gap: 14,
    padding: '8px 14px',
    borderTop: '1px solid var(--border-light)',
    flexShrink: 0,
    fontSize: 11,
    // The hint words are prose (DM Sans); the Kbd chips stay mono.
    color: 'var(--text-muted)',
  },
  footerHint: { display: 'inline-flex', alignItems: 'center', gap: 6 },
}
