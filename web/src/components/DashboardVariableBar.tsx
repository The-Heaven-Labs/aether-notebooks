import { useEffect, useState } from 'react'
import { RotateCcw, SlidersHorizontal } from 'lucide-react'
import { useDashboardVariables, isVariableDefault } from '../contexts/DashboardVariablesContext'
import { VariableSelect } from './VariableSelect'
import type { DashboardVariable } from '../types'

/** Local (not UTC) YYYY-MM-DD, so quick ranges don't shift at day boundaries. */
function localISODate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

/** Returns the preset length when the range is "last N days ending today". */
function quickRangePreset(range: string[]): number | null {
  if (range.length !== 2 || !range[0] || !range[1]) return null
  const [sy, sm, sd] = range[0].split('-').map(Number)
  const [ey, em, ed] = range[1].split('-').map(Number)
  if (!sy || !sm || !sd || !ey || !em || !ed) return null
  const start = new Date(sy, sm - 1, sd)
  const end = new Date(ey, em - 1, ed)
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  if (end.getTime() !== today.getTime()) return null
  const days = Math.round((end.getTime() - start.getTime()) / 86400000) + 1
  return days === 7 || days === 30 || days === 90 ? days : null
}

const styles: Record<string, React.CSSProperties> = {
  card: {
    // No card chrome inside the strip — nested cards would double the frame.
    // Each cluster is just an engraved label over its control.
    minWidth: 170,
    flex: '0 1 auto',
    display: 'flex',
    flexDirection: 'column',
    gap: 4,
  },
  cardTop: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
    minWidth: 0,
  },
  topHint: {
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap',
    flexShrink: 0,
  },
  label: {
    fontSize: 10,
    fontWeight: 700,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
    textTransform: 'uppercase',
    letterSpacing: '0.08em',
  },
  input: {
    padding: '7px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    width: '100%',
    boxSizing: 'border-box',
  },
  rangeRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    flexWrap: 'wrap',
  },
  rangeSep: {
    fontSize: 12,
    color: 'var(--text-muted)',
    flexShrink: 0,
  },
  quickRow: {
    display: 'flex',
    gap: 4,
    marginLeft: 'auto',
  },
  quickBtn: {
    padding: '2px 8px',
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
  },
  quickBtnActive: {
    background: 'var(--accent-light)',
    borderColor: 'var(--accent)',
    color: 'var(--accent)',
  },
  hint: {
    fontSize: 11,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
  warn: {
    fontSize: 11,
    color: 'var(--warning-text)',
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    flexWrap: 'wrap',
  },
  actionLink: {
    background: 'none',
    border: 'none',
    color: 'var(--accent)',
    cursor: 'pointer',
    fontSize: 11,
    fontWeight: 600,
    padding: 0,
    textDecoration: 'underline',
  },
  resetAll: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 5,
    padding: '6px 10px',
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
  },
  error: {
    fontSize: 12,
    color: 'var(--danger, #d33)',
    display: 'flex',
    alignItems: 'center',
    gap: 6,
  },
  retry: {
    background: 'none',
    border: 'none',
    color: 'var(--accent)',
    cursor: 'pointer',
    fontSize: 12,
    padding: 0,
  },
}

function TextControl({ variable, value, onChange }: {
  variable: DashboardVariable
  value: unknown
  onChange: (value: unknown) => void
}) {
  const committed = typeof value === 'string' ? value : ''
  const [draft, setDraft] = useState(committed)
  useEffect(() => {
    setDraft(committed)
  }, [committed])
  const dirty = draft !== committed
  const commit = () => { if (dirty) onChange(draft) }
  return (
    <>
      <input
        id={`dash-var-${variable.name}`}
        type="text"
        style={styles.input}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => { if (e.key === 'Enter') commit() }}
      />
      {dirty && <span style={styles.hint}>Press Enter to apply</span>}
    </>
  )
}

function VariableControl({ variable }: { variable: DashboardVariable }) {
  const { values, setValue, optionState } = useDashboardVariables()
  const value = values[variable.name]
  const state = optionState[variable.name]
  const id = `dash-var-${variable.name}`

  switch (variable.type) {
    case 'text':
      return <TextControl variable={variable} value={value} onChange={(v) => setValue(variable.name, v)} />
    case 'number':
      return (
        <input
          id={id}
          type="number"
          style={styles.input}
          value={value === undefined || value === null ? '' : String(value)}
          onChange={(e) => setValue(variable.name, e.target.value === '' ? undefined : Number(e.target.value))}
        />
      )
    case 'boolean':
      return (
        <input
          id={id}
          type="checkbox"
          checked={value === true}
          style={{ width: 16, height: 16, alignSelf: 'flex-start' }}
          onChange={(e) => setValue(variable.name, e.target.checked)}
        />
      )
    case 'date':
      return (
        <input
          id={id}
          type="date"
          style={styles.input}
          value={typeof value === 'string' ? value : ''}
          onChange={(e) => setValue(variable.name, e.target.value)}
        />
      )
    case 'date_range': {
      const range = Array.isArray(value) ? value.map(String) : ['', '']
      const setQuickRange = (days: number) => {
        const end = new Date()
        const start = new Date(end.getFullYear(), end.getMonth(), end.getDate() - (days - 1))
        setValue(variable.name, [localISODate(start), localISODate(end)])
      }
      const activePreset = quickRangePreset(range)
      return (
        <div style={styles.rangeRow}>
          <input
            id={id}
            type="date"
            aria-label={`${variable.label || variable.name} start`}
            style={{ ...styles.input, flex: '1 1 88px', minWidth: 0 }}
            value={range[0] ?? ''}
            onChange={(e) => setValue(variable.name, [e.target.value, range[1] ?? ''])}
          />
          <span style={styles.rangeSep}>to</span>
          <input
            type="date"
            aria-label={`${variable.label || variable.name} end`}
            style={{ ...styles.input, flex: '1 1 88px', minWidth: 0 }}
            value={range[1] ?? ''}
            onChange={(e) => setValue(variable.name, [range[0] ?? '', e.target.value])}
          />
          <div style={styles.quickRow}>
            {[7, 30, 90].map((days) => (
              <button
                key={days}
                type="button"
                style={{ ...styles.quickBtn, ...(activePreset === days ? styles.quickBtnActive : {}) }}
                title={`Last ${days} days`}
                aria-pressed={activePreset === days}
                onClick={() => setQuickRange(days)}
              >
                {days}d
              </button>
            ))}
          </div>
        </div>
      )
    }
    case 'single_select': {
      const options = state?.options ?? []
      // "All" is a first-class choice, like Grafana's include-all option; it
      // maps back to "no value set" for the query builder.
      const withAll = variable.required ? options : [{ label: 'All', value: '' }, ...options]
      return (
        <VariableSelect
          id={id}
          options={withAll}
          value={typeof value === 'string' ? value : ''}
          onChange={(v) => setValue(variable.name, v === '' ? undefined : v)}
          emptyLabel={variable.required ? 'Select…' : 'All'}
          disabled={state?.loading}
          ariaLabel={variable.label || variable.name}
        />
      )
    }
    case 'multi_select': {
      const selected = Array.isArray(value) ? value.map(String) : []
      const options = state?.options ?? []
      const isEmpty = selected.length === 0
      return (
        <>
          <VariableSelect
            id={id}
            multiple
            options={options}
            value={selected}
            onChange={(v) => setValue(variable.name, Array.isArray(v) ? v : [v])}
            disabled={state?.loading}
            ariaLabel={variable.label || variable.name}
          />
          {isEmpty && !state?.loading && (
            // An empty selection is an explicit "no rows" filter server-side;
            // say so at the source instead of letting widgets show silent zeros.
            <span style={styles.warn} role="status">
              Nothing selected — this hides all rows.
              {options.length > 0 && (
                <button
                  type="button"
                  style={styles.actionLink}
                  onClick={() => setValue(variable.name, options.map((o) => o.value))}
                >
                  Select all
                </button>
              )}
            </span>
          )}
        </>
      )
    }
    default:
      return null
  }
}

export function DashboardVariableBar() {
  const { variables, values, resetAll, optionState } = useDashboardVariables()
  if (!variables.length) return null
  const anyActive = variables.some((v) => !isVariableDefault(v, values[v.name]))
  return (
    <section className="dash-vars-bar" aria-label="Dashboard filters">
      <div className="dash-vars-bar-head">
        <SlidersHorizontal size={12} style={{ color: 'var(--accent)', flexShrink: 0 }} aria-hidden="true" />
        <span className="dash-vars-bar-label">Filters</span>
        {anyActive && (
          <button
            type="button"
            className="dash-vars-reset"
            style={styles.resetAll}
            onClick={resetAll}
            title="Restore every filter to its default"
          >
            <RotateCcw size={11} /> Reset filters
          </button>
        )}
      </div>
      <div className="dash-vars-fields">
        {variables.map((variable) => {
          const state = optionState[variable.name]
          const value = values[variable.name]
          const total = state?.options?.length ?? 0
          const countHint = variable.type === 'multi_select' && total > 0
            ? `${Array.isArray(value) ? value.length : 0} of ${total}`
            : null
          const wide = variable.type === 'multi_select' || variable.type === 'date_range'
          return (
            <div key={variable.name} className={wide ? 'dash-vars-card-wide' : undefined} style={styles.card}>
              <div style={styles.cardTop}>
                <label style={styles.label} htmlFor={`dash-var-${variable.name}`}>
                  {variable.label || variable.name}
                  {variable.required ? ' *' : ''}
                </label>
                {countHint && <span style={styles.topHint}>{countHint}</span>}
              </div>
              <VariableControl variable={variable} />
              {state?.loading && <span style={styles.hint}>Loading options…</span>}
              {state?.error && (
                <span style={styles.error} role="alert">
                  {state.error}
                  <button style={styles.retry} onClick={state.reload}>Retry</button>
                </span>
              )}
            </div>
          )
        })}
      </div>
    </section>
  )
}
