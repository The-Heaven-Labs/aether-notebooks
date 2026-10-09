import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown, Search } from 'lucide-react'

export interface VariableSelectOption {
  label: string
  value: string
}

interface Props {
  /** Matches the id of the field's <label>. */
  id: string
  options: VariableSelectOption[]
  value: string | string[]
  onChange: (value: string | string[]) => void
  /** Multi-select toggles checkboxes and stays open; single picks once and closes. */
  multiple?: boolean
  disabled?: boolean
  /** Shown for a single select with no value (defaults to "All"). */
  emptyLabel?: string
  ariaLabel?: string
}

const SEARCH_THRESHOLD = 8

/**
 * The dashboard parameter control: a compact trigger that summarizes its
 * selection ("All", "3 of 20", "None") and opens an anchored popover with
 * search, All/Clear, and checkboxes. Grafana's behavior in Aether's clothes.
 */
export function VariableSelect({ id, options, value, onChange, multiple = false, disabled = false, emptyLabel = 'All', ariaLabel }: Props) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const [pos, setPos] = useState<{ left: number; width: number; top?: number; bottom?: number } | null>(null)

  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const popRef = useRef<HTMLDivElement | null>(null)
  const searchRef = useRef<HTMLInputElement | null>(null)
  const listRef = useRef<HTMLDivElement | null>(null)
  const restoreFocusRef = useRef(false)

  const selected = useMemo(() => (multiple ? (Array.isArray(value) ? value : []) : []), [multiple, value])
  const selectedSet = useMemo(() => new Set(selected), [selected])
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? options.filter(o => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q)) : options
  }, [options, query])
  const showSearch = options.length >= SEARCH_THRESHOLD
  const listId = `${id}-listbox`

  const triggerLabel = useMemo(() => {
    if (!options.length) return 'No options'
    if (!multiple) {
      const v = typeof value === 'string' ? value : ''
      const match = options.find(o => o.value === v)
      return match ? match.label : emptyLabel
    }
    if (!selected.length) return 'None'
    if (selected.length === options.length) return 'All'
    if (selected.length <= 3) {
      return selected
        .map(v => options.find(o => o.value === v)?.label ?? v)
        .join(', ')
    }
    return `${selected.length} of ${options.length} selected`
  }, [options, multiple, value, selected, emptyLabel])

  const close = useCallback((restoreFocus: boolean) => {
    restoreFocusRef.current = restoreFocus
    setOpen(false)
  }, [])

  // Restore focus to the trigger only for keyboard-driven closes.
  useEffect(() => {
    if (!open && restoreFocusRef.current) {
      restoreFocusRef.current = false
      triggerRef.current?.focus()
    }
  }, [open])

  // Anchored placement, re-measured on scroll/resize while open.
  useLayoutEffect(() => {
    if (!open) return
    const place = () => {
      const r = triggerRef.current?.getBoundingClientRect()
      if (!r) return
      const width = Math.min(Math.max(r.width, 220), 340)
      const spaceBelow = window.innerHeight - r.bottom - 12
      const openUp = spaceBelow < 240 && r.top > spaceBelow
      setPos({
        left: Math.max(8, Math.min(r.left, window.innerWidth - width - 8)),
        width,
        top: openUp ? undefined : r.bottom + 4,
        bottom: openUp ? window.innerHeight - r.top + 4 : undefined,
      })
    }
    place()
    const onMove = () => place()
    window.addEventListener('resize', onMove)
    window.addEventListener('scroll', onMove, true)
    return () => {
      window.removeEventListener('resize', onMove)
      window.removeEventListener('scroll', onMove, true)
    }
  }, [open])

  // Outside click closes without stealing focus.
  useEffect(() => {
    if (!open) return
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node
      if (triggerRef.current?.contains(t) || popRef.current?.contains(t)) return
      close(false)
    }
    document.addEventListener('pointerdown', onDown, true)
    return () => document.removeEventListener('pointerdown', onDown, true)
  }, [open, close])

  // Focus target on open + keep the active row in view.
  useEffect(() => {
    if (!open) return
    setQuery('')
    setActiveIndex(0)
    const focusTimer = window.setTimeout(() => {
      if (showSearch) searchRef.current?.focus()
      else listRef.current?.focus()
    }, 0)
    return () => window.clearTimeout(focusTimer)
  }, [open, showSearch])

  useEffect(() => {
    if (!open) return
    const el = popRef.current?.querySelector<HTMLElement>(`[data-index="${activeIndex}"]`)
    el?.scrollIntoView?.({ block: 'nearest' })
  }, [activeIndex, open, filtered])

  const toggleMulti = (v: string) => {
    const next = selectedSet.has(v) ? selected.filter(s => s !== v) : [...selected, v]
    onChange(next)
  }

  const pick = (v: string) => {
    if (multiple) toggleMulti(v)
    else {
      onChange(v)
      close(true)
    }
  }

  const selectAll = () => onChange(options.map(o => o.value))
  const clearAll = () => onChange([])

  const onPopoverKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation()
      close(true)
      return
    }
    const count = filtered.length
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (count) setActiveIndex(i => (i + 1) % count)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (count) setActiveIndex(i => (i - 1 + count) % count)
    } else if (e.key === 'Home') {
      e.preventDefault()
      setActiveIndex(0)
    } else if (e.key === 'End') {
      e.preventDefault()
      if (count) setActiveIndex(count - 1)
    } else if (e.key === 'Enter' || e.key === ' ') {
      if (!count) return
      // Space inside the search field must type, not toggle.
      if (e.key === ' ' && e.target === searchRef.current) return
      e.preventDefault()
      const opt = filtered[activeIndex]
      if (opt) pick(opt.value)
    }
  }

  const popover = open && pos && (
    <div
      ref={popRef}
      style={{
        ...styles.popover,
        left: pos.left,
        width: pos.width,
        ...(pos.top != null ? { top: pos.top } : { bottom: pos.bottom }),
      }}
      onKeyDown={onPopoverKeyDown}
    >
      {showSearch && (
        <div style={styles.searchRow}>
          <Search size={12} style={styles.searchIcon} />
          <input
            ref={searchRef}
            style={styles.search}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search…"
            aria-label={`Search ${ariaLabel ?? 'options'}`}
            autoComplete="off"
          />
        </div>
      )}
      {multiple && (
        <div style={styles.bulkRow}>
          <button type="button" style={styles.bulkBtn} onClick={selectAll}>All</button>
          <button type="button" style={styles.bulkBtn} onClick={clearAll}>Clear</button>
          <span style={styles.bulkCount}>{selected.length} of {options.length}</span>
        </div>
      )}
      <div
        ref={listRef}
        id={listId}
        role="listbox"
        aria-multiselectable={multiple || undefined}
        aria-label={`${ariaLabel ?? id} options`}
        tabIndex={-1}
        style={styles.list}
      >
        {filtered.map((opt, i) => {
          const isSelected = multiple ? selectedSet.has(opt.value) : opt.value === (typeof value === 'string' ? value : '')
          return (
            <div
              key={opt.value}
              role="option"
              aria-selected={isSelected}
              data-index={i}
              style={{ ...styles.option, ...(i === activeIndex ? styles.optionActive : {}) }}
              onPointerDown={(e) => e.preventDefault()}
              onClick={() => pick(opt.value)}
              onMouseEnter={() => setActiveIndex(i)}
              title={opt.label}
            >
              <span style={{ ...styles.checkbox, ...(isSelected ? styles.checkboxOn : {}) }}>
                {isSelected && <Check size={10} strokeWidth={3} />}
              </span>
              <span style={styles.optionLabel}>{opt.label}</span>
            </div>
          )
        })}
        {filtered.length === 0 && <div style={styles.noOptions}>No matching options</div>}
      </div>
    </div>
  )

  return (
    <>
      <button
        type="button"
        id={id}
        ref={triggerRef}
        style={{ ...styles.trigger, ...(disabled ? styles.triggerDisabled : {}) }}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={() => setOpen(o => !o)}
        onKeyDown={(e) => {
          if (!open && (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ')) {
            e.preventDefault()
            setOpen(true)
          }
        }}
      >
        <span style={styles.triggerText}>{triggerLabel}</span>
        <ChevronDown size={12} style={{ flexShrink: 0, color: 'var(--text-muted)' }} />
      </button>
      {popover && createPortal(popover, document.body)}
    </>
  )
}

const styles: Record<string, React.CSSProperties> = {
  trigger: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 6,
    padding: '7px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    width: '100%',
    boxSizing: 'border-box',
    cursor: 'pointer',
    textAlign: 'left',
  },
  triggerDisabled: { opacity: 0.6, cursor: 'default' },
  triggerText: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  popover: {
    position: 'fixed',
    zIndex: 1300,
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    boxShadow: 'var(--shadow-md)',
    display: 'flex',
    flexDirection: 'column',
    maxHeight: 320,
    overflow: 'hidden',
  },
  searchRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '8px 10px',
    borderBottom: '1px solid var(--border-light)',
    flexShrink: 0,
  },
  searchIcon: { color: 'var(--text-muted)', flexShrink: 0 },
  search: {
    border: 'none',
    outline: 'none',
    background: 'none',
    color: 'var(--text-primary)',
    fontSize: 13,
    width: '100%',
    fontFamily: 'var(--font-sans)',
  },
  bulkRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '6px 10px',
    borderBottom: '1px solid var(--border-light)',
    flexShrink: 0,
  },
  bulkBtn: {
    background: 'none',
    border: 'none',
    padding: 0,
    color: 'var(--accent)',
    cursor: 'pointer',
    fontSize: 12,
    fontWeight: 600,
  },
  bulkCount: {
    marginLeft: 'auto',
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap',
  },
  list: { overflowY: 'auto', padding: '4px 0', outline: 'none' },
  option: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '6px 10px',
    fontSize: 13,
    color: 'var(--text-primary)',
    cursor: 'pointer',
    minHeight: 28,
  },
  optionActive: { background: 'var(--bg-input)' },
  checkbox: {
    width: 14,
    height: 14,
    borderRadius: 3,
    border: '1px solid var(--border)',
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
    color: 'var(--button-primary-text)',
  },
  checkboxOn: { background: 'var(--accent)', borderColor: 'var(--accent)' },
  optionLabel: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  noOptions: { padding: '10px 12px', fontSize: 12, color: 'var(--text-muted)', fontStyle: 'italic' },
}
