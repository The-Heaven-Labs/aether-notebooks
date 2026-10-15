import { memo, useEffect, useRef, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { Bookmark, Search } from 'lucide-react'
import { ApiError } from '../api/client'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { FavoriteButton } from '../components/FavoriteButton'
import { SectionHeader } from '../components/SectionHeader'
import { Skeleton } from '../components/Skeleton'
import { useFavorites } from '../hooks/useFavorites'
import {
  MAX_SEARCH_QUERY_LENGTH, SEARCH_QUERY_TOO_LONG_MESSAGE, useResourceSearch,
} from '../hooks/useResourceSearch'
import { HighlightedText } from '../utils/highlight'
import { isKnownType, resourceHref, TYPE_ICONS, TYPE_LABELS } from '../utils/resources'
import type { FavoriteResourceType, ResourceSummary } from '../types'

/** Results requested per type on the page; the API caps `limit` at 20. */
const PAGE_LIMIT = 20
/** Keystroke settle time before the URL's `q` (and the request) updates. */
const TYPING_DEBOUNCE_MS = 200

/**
 * A debounce write's marker in the router location state. Read back
 * shape-checked: the state slot is shared with every other navigator.
 */
function ownWriteToken(state: unknown): number | null {
  if (typeof state !== 'object' || state === null) return null
  const value = (state as { searchWrite?: unknown }).searchWrite
  return typeof value === 'number' ? value : null
}

/**
 * `/search` results page. The URL is the state: `q` (the query), `type` (one
 * type filter chip; absent = All), and `favorites=1`. Typing debounces into the
 * URL replace-style so a search session cannot bury the back button; chips
 * push, so back/forward restores the filtered view.
 */
export function SearchPage() {
  useEffect(() => { document.title = 'Search — Aether Notebooks' }, [])

  const [searchParams, setSearchParams] = useSearchParams()
  const { key: locationKey, state: locationState } = useLocation()
  const urlQuery = searchParams.get('q') ?? ''
  const typeParam = searchParams.get('type') ?? ''
  const activeType: FavoriteResourceType | null = isKnownType(typeParam) ? typeParam : null
  const favoritesOnly = searchParams.get('favorites') === '1'

  const [inputValue, setInputValue] = useState(urlQuery)
  // Mirrors the sync effects read: the import effect reacts to location changes
  // only (a keystroke must never re-trigger it) and the debounce timer checks
  // the settled URL at fire time.
  const inputValueRef = useRef(inputValue)
  const urlQueryRef = useRef(urlQuery)
  // Monotonic token stamped on every debounce write's navigation state. The
  // router commits navigations inside `startTransition`, so our own write's
  // commit can land after a newer keystroke; the import effect must recognize
  // that commit as ours and never import it over the keystroke.
  const writeTokenSeq = useRef(0)
  const lastWriteToken = useRef<number | null>(null)

  // Declared before both sync effects so they read fresh mirrors in the commit
  // that changed them.
  useEffect(() => {
    inputValueRef.current = inputValue
    urlQueryRef.current = urlQuery
  }, [inputValue, urlQuery])

  // The URL is the source of truth. Any location change — a chip push, a
  // back/forward, a deep link, the palette's "View all results", a pop between
  // two entries with an identical search string — wins over an un-debounced
  // draft; importing also cancels the pending debounce timer via the input
  // state change. The location *entry* (`key`) is the dep, not just the search
  // string: `useSearchParams` memoizes on `location.search`, so an
  // identical-URL pop changes neither `urlQuery` nor `searchParams` identity
  // and a string-gated effect would let the pending timer rewrite that entry,
  // cancelling Back. Our own writes are recognized by their navigation-state
  // token and skipped (consumed once, so a later pop to that entry imports
  // normally); typing cannot re-trigger anything, so the two can never loop.
  useEffect(() => {
    const token = ownWriteToken(locationState)
    if (token !== null && token === lastWriteToken.current) {
      lastWriteToken.current = null
      return
    }
    if (urlQuery === inputValueRef.current.trim()) return
    setInputValue(urlQuery)
  }, [urlQuery, searchParams, locationKey, locationState])

  // 200ms debounce: the input stays instant, the URL (and the request keyed by
  // it) follows the settled value, replace-style. Skipping when the URL has
  // already settled on this value keeps the initial mount and an imported URL
  // from rewriting their own entry.
  useEffect(() => {
    const next = inputValue.trim()
    const timer = window.setTimeout(() => {
      if (next === urlQueryRef.current) return
      const token = ++writeTokenSeq.current
      lastWriteToken.current = token
      setSearchParams((current) => {
        const params = new URLSearchParams(current)
        if (next) params.set('q', next)
        else params.delete('q')
        return params
      }, { replace: true, state: { searchWrite: token } })
    }, TYPING_DEBOUNCE_MS)
    return () => window.clearTimeout(timer)
  }, [inputValue, setSearchParams])

  const search = useResourceSearch({
    query: urlQuery,
    types: activeType ? [activeType] : undefined,
    favorites: favoritesOnly,
    limit: PAGE_LIMIT,
  })
  const { ids: favoriteIds, isLoading: favoritesLoading, isError: favoritesError } = useFavorites()

  const searching = urlQuery.trim().length > 0
  const filtersActive = activeType !== null || favoritesOnly
  // Defensive type guards, same as the palette: a retired type would have no
  // icon and no route, so it is skipped rather than rendered broken.
  const serverGroups = (search.data?.groups ?? [])
    .filter((group) => isKnownType(group.resource_type))
    .map((group) => ({
      ...group,
      results: group.results.filter((result) => isKnownType(result.resource_type)),
    }))
    .filter((group) => group.results.length > 0)
  // Under favorites=1 the server already restricts the response, but a toggle
  // flipped in this session must drop the row (and the count) immediately:
  // filter through the shared favorites cache. While that cache loads or
  // errors, the server's response is trusted as-is — a missing listing must
  // never blank results that are demonstrably there.
  const groups = favoritesOnly && !favoritesLoading && !favoritesError
    ? serverGroups
        .map((group) => ({
          ...group,
          results: group.results.filter((result) => favoriteIds[result.resource_type].has(result.id)),
        }))
        .filter((group) => group.results.length > 0)
    : serverGroups
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
    // Clicking the active chip is a no-op: never push a duplicate history entry
    // (it would also be indistinguishable from the current one).
    if (type === activeType) return
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
      // Pristine page (no query, no filters): the orientation panel. A filter
      // set with no query — hand-crafted URL, or the query cleared while a
      // chip stayed active — is a no-results state instead, so the chips and
      // the "no results" claim never layer over the orientation copy.
      if (!filtersActive) {
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
      return (
        <EmptyState
          title="No results"
          text="Type a query to search, or clear the filters above."
        />
      )
    }
    if (search.isError) {
      // The API's only user-correctable rejection here is an over-long query.
      const message = search.error instanceof ApiError && search.error.status === 400
        ? SEARCH_QUERY_TOO_LONG_MESSAGE
        : 'Search failed.'
      return (
        <div role="alert" style={styles.errorCard}>
          <span>{message}</span>
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
        <div style={styles.noticeWrap}>
          <EmptyState
            title={`No matches for “${urlQuery}”`}
            text="Try a different word or a shorter query."
          />
        </div>
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

  const showChrome = searching || filtersActive
  // The live region announces outcomes; an idle, loading, or failed search has
  // none, so it renders empty (the element stays mounted to keep announcing).
  const statusText = !searching || search.isError || (search.isLoading && !search.data)
    ? ''
    : resultCount === 0
      ? 'No results'
      : `${resultCount}${hasMore ? '+' : ''} result${resultCount === 1 && !hasMore ? '' : 's'}`

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
            maxLength={MAX_SEARCH_QUERY_LENGTH}
            style={styles.input}
          />
        </div>

        {showChrome && (
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
        )}

        {/* Always mounted: AT hears every count change (and the empty→filled
            transitions), not just the first result set. */}
        <p style={styles.status} role="status" aria-live="polite">{statusText}</p>

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
    // No `outline: none` here: the global `input:focus-visible` ring is the
    // primary control's focus affordance.
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
  // The no-results title names the query, which can be one long unbroken token
  // (a pasted SQL line): let it break anywhere instead of overflowing the page.
  noticeWrap: { overflowWrap: 'anywhere' },
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
