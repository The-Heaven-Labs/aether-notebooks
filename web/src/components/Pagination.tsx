import { ChevronLeft, ChevronRight } from 'lucide-react'

interface Props {
  page: number        // 0-indexed current page
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}

export function Pagination({ page, pageSize, total, onPageChange }: Props) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const startItem = total === 0 ? 0 : page * pageSize + 1
  const endItem = Math.min((page + 1) * pageSize, total)

  // Build page number array with ellipsis
  const pages = buildPageNumbers(page, totalPages)

  return (
    <div style={styles.container}>
      <span style={styles.info}>
        Showing {startItem}–{endItem} of {total.toLocaleString()} entries
      </span>
      <div style={styles.controls}>
        <button
          style={{ ...styles.btn, opacity: page === 0 ? 0.3 : 1 }}
          onClick={() => onPageChange(page - 1)}
          disabled={page === 0}
          aria-label="Previous page"
        >
          <ChevronLeft size={14} />
        </button>
        {pages.map((p, i) =>
          p === '…' ? (
            <span key={`ellipsis-${i}`} style={styles.ellipsis}>…</span>
          ) : (
            <button
              key={p}
              style={{
                ...styles.pageBtn,
                ...(p === page ? styles.pageBtnActive : {}),
              }}
              onClick={() => onPageChange(p as number)}
            >
              {(p as number) + 1}
            </button>
          )
        )}
        <button
          style={{ ...styles.btn, opacity: page >= totalPages - 1 ? 0.3 : 1 }}
          onClick={() => onPageChange(page + 1)}
          disabled={page >= totalPages - 1}
          aria-label="Next page"
        >
          <ChevronRight size={14} />
        </button>
      </div>
    </div>
  )
}

/** Build array of page numbers (0-indexed) with '…' for gaps */
function buildPageNumbers(current: number, total: number): (number | '…')[] {
  if (total <= 7) {
    return Array.from({ length: total }, (_, i) => i)
  }

  const pages: (number | '…')[] = [0]

  if (current > 2) pages.push('…')

  const start = Math.max(1, current - 1)
  const end = Math.min(total - 2, current + 1)

  for (let i = start; i <= end; i++) {
    pages.push(i)
  }

  if (current < total - 3) pages.push('…')

  pages.push(total - 1)

  return pages
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 0',
    gap: 16,
    flexWrap: 'wrap',
  },
  info: {
    fontSize: 12,
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
  },
  controls: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
  },
  btn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '4px 8px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
  pageBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    minWidth: 32,
    height: 32,
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
    cursor: 'pointer',
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
  },
  pageBtnActive: {
    background: 'var(--accent)',
    color: '#fff',
    borderColor: 'var(--accent)',
  },
  ellipsis: {
    padding: '0 4px',
    fontSize: 14,
    color: 'var(--text-muted)',
    userSelect: 'none',
  },
}
