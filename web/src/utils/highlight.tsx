import type React from 'react'

/** Escapes one term so regex metacharacters in the query match literally. */
function escapeRegExp(term: string): string {
  return term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * Splits `text` into plain strings and `<mark className="search-hit">` nodes
 * around every case-insensitive occurrence of any whitespace-separated query
 * term. Returns React nodes, never an HTML string, so matched text can never
 * inject markup. An empty query or no matches return the original text as a
 * single node.
 */
// eslint-disable-next-line react-refresh/only-export-components -- the helper and its two-line renderer belong together for the search surfaces
export function highlight(text: string, query: string): React.ReactNode[] {
  const terms = query
    .split(/\s+/)
    .filter(Boolean)
    // Longest first: a shorter term that prefixes a longer one cannot split it.
    .sort((a, b) => b.length - a.length)
    .map(escapeRegExp)
  if (!terms.length || !text) return [text]

  const matcher = new RegExp(`(${terms.join('|')})`, 'gi')
  const nodes: React.ReactNode[] = []
  let cursor = 0
  let hit = 0
  for (const match of text.matchAll(matcher)) {
    const index = match.index ?? 0
    if (index > cursor) nodes.push(text.slice(cursor, index))
    nodes.push(
      <mark key={`search-hit-${hit++}`} className="search-hit">{match[0]}</mark>,
    )
    cursor = index + match[0].length
  }
  if (cursor < text.length) nodes.push(text.slice(cursor))
  return nodes.length ? nodes : [text]
}

/** Inline renderer for {@link highlight}; the search surfaces' title wrapper. */
export function HighlightedText({ text, query }: { text: string; query: string }) {
  return <>{highlight(text, query)}</>
}
