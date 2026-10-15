import { memo, useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Bookmark, Search } from 'lucide-react'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { FavoriteButton } from '../components/FavoriteButton'
import { SectionHeader } from '../components/SectionHeader'
import { Skeleton } from '../components/Skeleton'
import { useResourceSearch } from '../hooks/useResourceSearch'
import { HighlightedText } from '../utils/highlight'
import { isKnownType, resourceHref, TYPE_ICONS, TYPE_LABELS } from '../utils/resources'
import type { FavoriteResourceType, ResourceSummary } from '../types'

/** Results requested per type on the page; the API caps `limit` at 20. */
const PAGE_LIMIT = 20
/** Keystroke settle time before the URL's `q` (and the request) updates. */
const TYPING_DEBOUNCE_MS = 200

/**
 * `/search` results page. The URL is the state: `q` (the query), `type` (one
 * type filter chip; absent = All), and `favorites=1`. Typing debounces into the
 * URL replace-style so a search session cannot bury the back button; chips
 * push, so back/forward restores the filtered view.
 */
export function SearchPage() {
  useEffect(() => { document.title = 'Search — Aether Notebooks' }, [])

  const [searchParams, setSearchParams] = useSearchParams()
  const urlQuery = searchParams.get('q') ?? ''
  const typeParam = searchParams.get('type') ?? ''
  const activeType: FavoriteResourceType | null = isKnownType(typeParam) ? typeParam : null
  const favoritesOnly = searchParams.get('favorites') === '1'

  const [inputValue, setInputValue] = useState(urlQuery)
  // The last `q` this component wrote to the URL. A URL value that differs
  // arrived from outside (back/forward, a shared link, the palette's "View all
  // results") and wins over the local typing state.
  const lastWrittenQuery = useRef(urlQuery)

  useEffect(() => {
    if (urlQuery === lastWrittenQuery.current) return
    lastWrittenQuery.current = urlQuery
    setInputValue(urlQuery)
  }, [urlQuery])

  // 200ms debounce: the input stays instant, the URL (and the request keyed by
  // it) follows the settled value, replace-style.
  useEffect(() => {
    const next = inputValue.trim()
    const timer = window.setTimeout(() => {
      if (next === lastWrittenQuery.current) return
      lastWrittenQuery.current = next
      setSearchParams((current) => {
        const params = new URLSearchParams(current)
        if (next) params.set('q', next)
        else params.delete('q')
        return params
      }, { replace: true })
    }, TYPING_DEBOUNCE_MS)
    return () => window.clearTimeout(timer)
  }, [inputValue, setSearchParams])

  const search = useResourceSearch({
    query: urlQuery,
    types: activeType ? [activeType] : undefined,
    favorites: favoritesOnly,
    limit: PAGE_LIMIT,
  })

  const searching = urlQuery.length > 0
  // Defensive type guards, same as the palette: a retired type would have no
  // icon and no route, so it is skipped rather than rendered broken.
  const groups = (search.data?.groups ?? [])
    .filter((group) => isKnownType(group.resource_type))
    .map((group) => ({
      ...group,
      results: group.results.filter((result) => isKnownType(result.resource_type)),
    }))
    .filter((group) => group.results.length > 0)
  const resultCount = groups.reduce((count, group) => count + group.results.length, 0)
  const hasMore = groups.some((group) => group.has_more)
  // Keep the highlight terms in step with the rendered rows while a debounced
  // query loads (keepPreviousData shows the previous response's matches).
  const highlightQuery = search.data?.query || urlQuery

  // Chips cover the types this response has results for, plus the active
  // filter (a type with no matches still needs its chip to read as active
  // and be clearable), in response order.
  const chipTypes: FavoriteResourceType[] = groups.map((group) => group.resource_type)
  if (activeType && !chipTypes.includes(activeType)) chipTypes.unshift(activeType)

  const setType = (type: FavoriteResourceType | null) => {
    setSearchParams((current) => {
      const params = new URLSearchParams(current)
      if (type) params.set('type', type)
      else params.delete('type')
      return params
    })
  }

  const toggleFavorites = () => {
    setSearchParams((current) => {
      const params = new URLSearchParams(current)
      if (params.get('favorites') === '1') params.delete('favorites')
      else params.set('favorites', '1')
      return params
    })
  }

  const renderBody = () => {
    if (!searching) {
      return (
        <>
          <EmptyState
            icon={<Search size={24} />}
            title="Search the workspace"
            text="Search across notebooks, dashboards, folders, connectors, agents, models, skills, tools, and MCP servers."
          />
          <p style={styles.hint}>Start typing to see results.</p>
        </>
      )
    }
    if (search.isError) {
      return (
        <div style={styles.errorCard}>
          <span>Search failed.</span>
          <button type="button" style={styles.retryBtn} onClick={() => { void search.refetch() }}>
            Retry
          </button>
        </div>
      )
    }
    if (search.isLoading && !search.data) {
      return (
        <div style={styles.skeletonWrap}>
          <Skeleton count={5} height={64} />
        </div>
      )
    }
    if (!groups.length) {
      return (
        <EmptyState
          title={`No matches for “${urlQuery}”`}
          text="Try a different word or a shorter query."
        />
      )
    }
    return (
      <>
        {groups.map((group) => (
          <section key={group.resource_type} style={styles.group}>
            <h3 style={styles.groupHeader}>
              {TYPE_LABELS[group.resource_type]} · {group.results.length}
            </h3>
            <div style={styles.rows}>
              {group.results.map((result) => (
                <SearchResultRow
                  key={`${group.resource_type}:${result.id}`}
                  result={result}
                  query={highlightQuery}
                />
              ))}
            </div>
          </section>
        ))}
        {hasMore && <p style={styles.more}>More matches — refine your search</p>}
      </>
    )
  }

  const showChrome = searching || activeType !== null || favoritesOnly

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader title="Search" />

        <div style={styles.searchField}>
          <Search size={18} style={styles.searchIcon} aria-hidden="true" />
          <input
            type="text"
            aria-label="Search"
            placeholder="Search files, dashboards, SQL…"
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            autoFocus={!searching}
            autoComplete="off"
            spellCheck={false}
            style={styles.input}
          />
        </div>

        {showChrome && (
          <>
            <div style={styles.chips} role="group" aria-label="Filter results">
              <button
                type="button"
                style={{ ...styles.chip, ...(activeType === null ? styles.chipActive : {}) }}
                aria-pressed={activeType === null}
                onClick={() => setType(null)}
              >
                All
              </button>
              {chipTypes.map((type) => (
                <button
                  key={type}
                  type="button"
                  style={{ ...styles.chip, ...(activeType === type ? styles.chipActive : {}) }}
                  aria-pressed={activeType === type}
                  onClick={() => setType(type)}
                >
                  {TYPE_LABELS[type]}
                </button>
              ))}
              <button
                type="button"
                style={{ ...styles.chip, ...(favoritesOnly ? styles.chipActive : {}) }}
                aria-pressed={favoritesOnly}
                onClick={toggleFavorites}
              >
                <Bookmark size={12} fill={favoritesOnly ? 'currentColor' : 'none'} aria-hidden="true" />
                Favorites
              </button>
            </div>

            {/* AT announcement of result changes; also the visible count. */}
            {!search.isError && !(search.isLoading && !search.data) && (
              <p style={styles.status} role="status" aria-live="polite">
                {resultCount === 0 ? 'No results' : `${resultCount} result${resultCount === 1 ? '' : 's'}`}
              </p>
            )}
          </>
        )}

        {renderBody()}
      </div>
    </AppShell>
  )
}

interface SearchResultRowProps {
  result: ResourceSummary
  /** The settled query the response was fetched for; drives the highlights. */
  query: string
}

/**
 * One result row at list scale: accent type icon, highlighted title, a
 * two-line mono snippet when content matched, folder path + subtitle meta,
 * and a bookmark toggle. Memoized — with up to 20 rows per type the row render
 * would otherwise repeat on every parent state change.
 */
const SearchResultRow = memo(function SearchResultRow({ result, query }: SearchResultRowProps) {
  const Icon = TYPE_ICONS[result.resource_type]
  const meta = [result.folder_path, result.subtitle].filter(Boolean).join(' · ')
  return (
    <div style={styles.row} className="card-hover">
      <Link to={resourceHref(result.resource_type, result.id)} style={styles.rowLink}>
        <Icon size={16} strokeWidth={2} style={styles.rowIcon} aria-hidden="true" />
        <div style={styles.rowBody}>
          <div style={styles.rowTitle}>
            <HighlightedText text={result.title} query={query} />
          </div>
          {result.snippet && (
            <div style={styles.rowSnippet}>
              <HighlightedText text={result.snippet} query={query} />
            </div>
          )}
          {meta && <div style={styles.rowMeta}>{meta}</div>}
        </div>
      </Link>
      {/* The toggle always renders beside the link, so the reserved 26px
          column keeps row width stable. */}
      <span style={styles.rowFavorite}>
        <FavoriteButton resourceType={result.resource_type} resourceId={result.id} />
      </span>
    </div>
  )
})

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1000, margin: '0 auto', padding: 'clamp(16px, 4vw, 32px)', width: '100%' },
  searchField: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '12px 14px',
    background: 'var(--bg-input)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    marginBottom: 14,
  },
  searchIcon: { color: 'var(--text-muted)', flexShrink: 0 },
  input: {
    flex: 1,
    minWidth: 0,
    background: 'none',
    border: 'none',
    outline: 'none',
    color: 'var(--text-primary)',
    fontSize: 16,
    fontFamily: 'var(--font-sans)',
    padding: 0,
  },
  chips: { display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 12 },
  chip: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    padding: '4px 12px',
    background: 'none',
    // Longhands only: `chipActive` overrides borderColor, and mixing with the
    // `border` shorthand makes React warn on every active-state swap.
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: 'var(--border)',
    borderRadius: 999,
    color: 'var(--text-secondary)',
    fontSize: 12,
    fontWeight: 500,
    cursor: 'pointer',
    whiteSpace: 'nowrap',
  },
  // Active = accent ink on a hairline, no fill (accent budget: ink only).
  chipActive: { borderColor: 'var(--accent)', color: 'var(--accent)' },
  status: {
    margin: '0 0 20px',
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    letterSpacing: '0.06em',
    textTransform: 'uppercase',
    color: 'var(--text-muted)',
  },
  group: { marginBottom: 28 },
  groupHeader: {
    margin: '0 0 8px',
    fontFamily: 'var(--font-mono)',
    fontSize: 10,
    fontWeight: 700,
    letterSpacing: '0.08em',
    textTransform: 'uppercase',
    color: 'var(--text-muted)',
  },
  rows: { display: 'flex', flexDirection: 'column', gap: 8 },
  row: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: '10px 16px',
  },
  rowLink: {
    flex: 1,
    display: 'flex',
    alignItems: 'flex-start',
    gap: 10,
    minWidth: 0,
    textDecoration: 'none',
  },
  rowIcon: { color: 'var(--accent)', flexShrink: 0, marginTop: 2 },
  rowBody: { flex: 1, minWidth: 0 },
  rowTitle: {
    fontSize: 14,
    fontWeight: 600,
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
    display: '-webkit-box',
    WebkitLineClamp: 2,
    WebkitBoxOrient: 'vertical',
    overflow: 'hidden',
  },
  rowMeta: {
    marginTop: 3,
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  rowFavorite: { flexShrink: 0, display: 'inline-flex' },
  more: { margin: '4px 0 0', fontSize: 12, color: 'var(--text-muted)' },
  hint: { margin: 0, textAlign: 'center', fontSize: 13, color: 'var(--text-muted)' },
  skeletonWrap: { display: 'flex', flexDirection: 'column', gap: 8 },
  errorCard: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    padding: '16px 16px',
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    color: 'var(--text-secondary)',
  },
  retryBtn: {
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontSize: 12,
    padding: '3px 10px',
  },
}
