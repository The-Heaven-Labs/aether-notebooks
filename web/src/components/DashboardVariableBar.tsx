import { useEffect, useState } from 'react'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import type { DashboardVariable } from '../types'

const styles: Record<string, React.CSSProperties> = {
  bar: {
    display: 'flex',
    flexWrap: 'wrap',
    gap: 12,
  },
  card: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    minWidth: 200,
    flex: '0 1 auto',
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    padding: '14px 16px',
  },
  label: {
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
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
    outline: 'none',
  },
  rangeRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  rangeSep: {
    fontSize: 12,
    color: 'var(--text-muted)',
    flexShrink: 0,
  },
  hint: {
    fontSize: 11,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
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
  const [draft, setDraft] = useState(typeof value === 'string' ? value : '')
  useEffect(() => {
    setDraft(typeof value === 'string' ? value : '')
  }, [value])
  const commit = () => onChange(draft)
  return (
    <input
      id={`dash-var-${variable.name}`}
      type="text"
      style={styles.input}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => { if (e.key === 'Enter') commit() }}
    />
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
      return (
        <div style={styles.rangeRow}>
          <input
            id={id}
            type="date"
            aria-label={`${variable.label || variable.name} start`}
            style={{ ...styles.input, flex: 1 }}
            value={range[0] ?? ''}
            onChange={(e) => setValue(variable.name, [e.target.value, range[1] ?? ''])}
          />
          <span style={styles.rangeSep}>to</span>
          <input
            type="date"
            aria-label={`${variable.label || variable.name} end`}
            style={{ ...styles.input, flex: 1 }}
            value={range[1] ?? ''}
            onChange={(e) => setValue(variable.name, [range[0] ?? '', e.target.value])}
          />
        </div>
      )
    }
    case 'single_select':
      return (
        <select
          id={id}
          style={styles.input}
          value={typeof value === 'string' ? value : ''}
          disabled={state?.loading}
          onChange={(e) => setValue(variable.name, e.target.value === '' ? undefined : e.target.value)}
        >
          {!variable.required && <option value="">All</option>}
          {(state?.options ?? []).map((opt) => (
            <option key={opt.value} value={opt.value}>{opt.label}</option>
          ))}
        </select>
      )
    case 'multi_select': {
      const selected = Array.isArray(value) ? value.map(String) : []
      return (
        <>
          <select
            id={id}
            multiple
            style={{ ...styles.input, height: 'auto', minHeight: 80 }}
            value={selected}
            onChange={(e) => setValue(variable.name, Array.from(e.target.selectedOptions).map((o) => o.value))}
          >
            {(state?.options ?? []).map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
          <span style={styles.hint}>Hold Ctrl / Cmd to select multiple</span>
        </>
      )
    }
    default:
      return null
  }
}

export function DashboardVariableBar() {
  const { variables, optionState } = useDashboardVariables()
  if (!variables.length) return null
  return (
    <div style={styles.bar}>
      {variables.map((variable) => {
        const state = optionState[variable.name]
        return (
          <div key={variable.name} style={styles.card}>
            <label style={styles.label} htmlFor={`dash-var-${variable.name}`}>
              {variable.label || variable.name}
              {variable.required ? ' *' : ''}
            </label>
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
  )
}
