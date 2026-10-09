import { useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Plus, Trash2, X } from 'lucide-react'
import { api } from '../api/client'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useFocusTrap } from '../hooks/useFocusTrap'
import { ConnectorSelector } from './ConnectorSelector'
import { SqlEditor } from './SqlEditor'
import type { Dashboard, DashboardVariable, DashboardVariableType } from '../types'

const VARIABLE_TYPES: DashboardVariableType[] = [
  'text', 'number', 'boolean', 'date', 'date_range', 'single_select', 'multi_select',
]

function isSelectType(type: DashboardVariableType): boolean {
  return type === 'single_select' || type === 'multi_select'
}

interface EditableVariable {
  name: string
  label: string
  type: DashboardVariableType
  defaultText: string
  defaultBool: boolean
  required: boolean
  mode: 'static' | 'query'
  optionsText: string
  queryConnectorId: string | null
  querySql: string
  labelColumn: string
  valueColumn: string
  dependsOn: string[]
}

function toEditable(v: DashboardVariable): EditableVariable {
  let defaultText = ''
  let defaultBool = false
  if (v.type === 'boolean') {
    defaultBool = v.default === true
  } else if ((v.type === 'date_range' || v.type === 'multi_select') && Array.isArray(v.default)) {
    defaultText = (v.default as unknown[]).map(String).join(',')
  } else if (v.default !== undefined && v.default !== null) {
    defaultText = String(v.default)
  }
  return {
    name: v.name,
    label: v.label ?? '',
    type: v.type,
    defaultText,
    defaultBool,
    required: !!v.required,
    mode: isSelectType(v.type) ? (v.options?.mode ?? 'static') : 'static',
    optionsText: (v.options?.values ?? []).map(o => `${o.label}=${o.value}`).join('\n'),
    queryConnectorId: v.options?.query?.connector_id ?? null,
    querySql: v.options?.query?.sql ?? '',
    labelColumn: v.options?.label_column ?? '',
    valueColumn: v.options?.value_column ?? '',
    dependsOn: v.depends_on ?? [],
  }
}

function emptyVariable(): EditableVariable {
  return {
    name: '', label: '', type: 'text', defaultText: '', defaultBool: false, required: false,
    mode: 'static', optionsText: '', queryConnectorId: null, querySql: '', labelColumn: '', valueColumn: '',
    dependsOn: [],
  }
}

function parseOptions(text: string): Array<{ label: string; value: string }> {
  return text.split('\n').map(line => line.trim()).filter(Boolean).map(line => {
    const idx = line.indexOf('=')
    if (idx < 0) return { label: line, value: line }
    return { label: line.slice(0, idx).trim(), value: line.slice(idx + 1).trim() }
  })
}

function fromEditable(e: EditableVariable): DashboardVariable {
  const out: DashboardVariable = { name: e.name.trim(), type: e.type }
  if (e.label.trim()) out.label = e.label.trim()
  if (e.required) out.required = true
  switch (e.type) {
    case 'boolean':
      out.default = e.defaultBool
      break
    case 'number':
      if (e.defaultText.trim() !== '') out.default = Number(e.defaultText)
      break
    case 'multi_select':
      if (e.defaultText.trim()) out.default = e.defaultText.split(',').map(s => s.trim()).filter(Boolean)
      break
    case 'date_range':
      if (e.defaultText.trim()) {
        const [start, end] = e.defaultText.split(',').map(s => s.trim())
        out.default = [start ?? '', end ?? '']
      }
      break
    default:
      if (e.defaultText !== '') out.default = e.defaultText
  }
  if (isSelectType(e.type)) {
    if (e.mode === 'query') {
      out.options = {
        mode: 'query',
        query: { connector_id: e.queryConnectorId ?? '', sql: e.querySql },
      }
      if (e.labelColumn.trim()) out.options.label_column = e.labelColumn.trim()
      if (e.valueColumn.trim()) out.options.value_column = e.valueColumn.trim()
    } else {
      out.options = { mode: 'static', values: parseOptions(e.optionsText) }
    }
  }
  if (e.dependsOn.length) out.depends_on = e.dependsOn
  return out
}

function validate(rows: EditableVariable[]): string | null {
  const names = new Set<string>()
  for (const row of rows) {
    const name = row.name.trim()
    if (!name) return 'Every variable needs a name'
    if (!/^[a-zA-Z0-9_-]+$/.test(name)) return `Invalid variable name "${name}" — use letters, numbers, "_" or "-"`
    if (names.has(name)) return `Duplicate variable name "${name}"`
    names.add(name)
    if (isSelectType(row.type) && row.mode === 'query' && (!row.queryConnectorId || !row.querySql.trim())) {
      return `Variable "${name}" needs a connector and SQL for query-backed options`
    }
  }
  for (const row of rows) {
    for (const dep of row.dependsOn) {
      if (!names.has(dep)) return `Variable "${row.name.trim()}" depends on unknown variable "${dep}"`
    }
  }
  return null
}

const styles: Record<string, React.CSSProperties> = {
  // The panel covers the top bar (z-index 1550/1600), so it must sit above it:
  // otherwise the top bar hides the header and intercepts clicks on the close button.
  backdrop: {
    position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.3)', zIndex: 1700,
  },
  panel: {
    position: 'fixed', top: 0, right: 0, bottom: 0, width: 480, maxWidth: '100vw',
    background: 'var(--bg-card)', borderLeft: '1px solid var(--border)',
    display: 'flex', flexDirection: 'column', zIndex: 1701,
    boxShadow: 'var(--shadow-lg, -4px 0 16px rgba(0,0,0,0.2))',
  },
  header: {
    display: 'flex', alignItems: 'center', justifyContent: 'space-between',
    padding: '14px 16px', borderBottom: '1px solid var(--border)', flexShrink: 0,
  },
  title: { fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' },
  close: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', padding: '2px 4px', display: 'flex', alignItems: 'center' },
  body: { padding: 16, display: 'flex', flexDirection: 'column', gap: 14, overflowY: 'auto', flex: 1 },
  row: {
    border: '1px solid var(--border)', borderRadius: 4, padding: 12,
    display: 'flex', flexDirection: 'column', gap: 8, background: 'var(--bg-card)',
  },
  rowHeader: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 },
  grid2: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 },
  label: { fontSize: 11, fontWeight: 600, color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' },
  input: {
    padding: '7px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13,
    background: 'var(--bg-input)', color: 'var(--text-primary)', width: '100%', boxSizing: 'border-box',
  },
  checkboxRow: { display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary)' },
  textarea: {
    padding: '7px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12,
    fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)',
    width: '100%', boxSizing: 'border-box', minHeight: 72, resize: 'vertical',
  },
  removeBtn: {
    background: 'none', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer',
    color: 'var(--text-muted)', padding: '3px 6px', display: 'flex', alignItems: 'center',
  },
  addBtn: {
    display: 'inline-flex', alignItems: 'center', gap: 6, alignSelf: 'flex-start',
    padding: '6px 12px', fontSize: 12, fontWeight: 600, background: 'var(--bg-input)',
    color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer',
  },
  saveBtn: {
    padding: '9px 0', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none',
    borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer',
  },
  error: { fontSize: 12, color: 'var(--danger, #d33)' },
  saved: { fontSize: 12, color: 'var(--success, #2a2)' },
  muted: { fontSize: 12, color: 'var(--text-muted)', fontStyle: 'italic' },
}

export function DashboardVariablesPanel({ dashboardId, dashboard, onClose, onSaved, initialNewName, initialNewType }: {
  dashboardId: string
  dashboard: Dashboard
  onClose: () => void
  onSaved: () => void
  initialNewName?: string | null
  /** Pre-selected type for the prefilled row (e.g. date_range from a token). */
  initialNewType?: DashboardVariableType | null
}) {
  const initialRows = useMemo(() => {
    const rows = (dashboard.settings?.variables ?? []).map(toEditable)
    if (initialNewName) rows.push({ ...emptyVariable(), name: initialNewName, ...(initialNewType ? { type: initialNewType } : {}) })
    return rows
  }, [dashboard.settings?.variables, initialNewName, initialNewType])

  const [rows, setRows] = useState<EditableVariable[]>(initialRows)
  const [publicLive, setPublicLive] = useState(!!dashboard.settings?.public_live)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const panelRef = useRef<HTMLDivElement>(null)

  useEscapeToClose(onClose)
  useFocusTrap(panelRef)

  const updateRow = (index: number, patch: Partial<EditableVariable>) => {
    setSaved(false)
    setRows(prev => prev.map((row, i) => (i === index ? { ...row, ...patch } : row)))
  }

  const allNames = rows.map(r => r.name.trim()).filter(Boolean)

  const save = async () => {
    const validationError = validate(rows)
    if (validationError) {
      setError(validationError)
      return
    }
    setSaving(true)
    setError(null)
    try {
      await api.put(`/api/v1/dashboards/${dashboardId}`, {
        settings: { ...dashboard.settings, variables: rows.map(fromEditable), public_live: publicLive },
      })
      setSaved(true)
      onSaved()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save variables')
    } finally {
      setSaving(false)
    }
  }

  return createPortal(
    <>
      <div data-testid="variables-backdrop" style={styles.backdrop} onClick={onClose} aria-hidden="true" />
      <div ref={panelRef} style={styles.panel} role="dialog" aria-modal="true" aria-label="Dashboard variables" tabIndex={-1}>
        <div style={styles.header}>
          <span style={styles.title}>Dashboard variables</span>
          <button type="button" style={styles.close} onClick={onClose} aria-label="Close variables panel">
            <X size={15} />
          </button>
        </div>
        <div style={styles.body}>
          {rows.length === 0 && (
            <span style={styles.muted}>No variables yet. Add one and reference it in query widgets as {'{{name}}'}.</span>
          )}

          {rows.map((row, index) => {
            const otherNames = allNames.filter(n => n !== row.name.trim())
            return (
              <div key={index} style={styles.row}>
                <div style={styles.rowHeader}>
                  <div style={styles.grid2}>
                    <div>
                      <label style={styles.label} htmlFor={`var-name-${index}`}>Name</label>
                      <input
                        id={`var-name-${index}`}
                        aria-label="Variable name"
                        style={styles.input}
                        value={row.name}
                        onChange={(e) => updateRow(index, { name: e.target.value })}
                      />
                    </div>
                    <div>
                      <label style={styles.label} htmlFor={`var-label-${index}`}>Label</label>
                      <input
                        id={`var-label-${index}`}
                        aria-label="Variable label"
                        style={styles.input}
                        value={row.label}
                        onChange={(e) => updateRow(index, { label: e.target.value })}
                      />
                    </div>
                  </div>
                  <button
                    type="button"
                    style={styles.removeBtn}
                    aria-label="Remove variable"
                    title="Remove variable"
                    onClick={() => { setSaved(false); setRows(prev => prev.filter((_, i) => i !== index)) }}
                  >
                    <Trash2 size={12} />
                  </button>
                </div>

                <div style={styles.grid2}>
                  <div>
                    <label style={styles.label} htmlFor={`var-type-${index}`}>Type</label>
                    <select
                      id={`var-type-${index}`}
                      aria-label="Variable type"
                      style={styles.input}
                      value={row.type}
                      onChange={(e) => updateRow(index, { type: e.target.value as DashboardVariableType })}
                    >
                      {VARIABLE_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
                    </select>
                  </div>
                  <div>
                    <label style={styles.label} htmlFor={`var-default-${index}`}>Default</label>
                    {row.type === 'boolean' ? (
                      <input
                        id={`var-default-${index}`}
                        aria-label="Variable default"
                        type="checkbox"
                        checked={row.defaultBool}
                        onChange={(e) => updateRow(index, { defaultBool: e.target.checked })}
                      />
                    ) : row.type === 'date' ? (
                      <input
                        id={`var-default-${index}`}
                        aria-label="Variable default"
                        type="date"
                        style={styles.input}
                        value={row.defaultText}
                        onChange={(e) => updateRow(index, { defaultText: e.target.value })}
                      />
                    ) : (
                      <input
                        id={`var-default-${index}`}
                        aria-label="Variable default"
                        style={styles.input}
                        placeholder={row.type === 'date_range' || row.type === 'multi_select' ? 'comma-separated' : ''}
                        value={row.defaultText}
                        onChange={(e) => updateRow(index, { defaultText: e.target.value })}
                      />
                    )}
                  </div>
                </div>

                {isSelectType(row.type) && (
                  <>
                    <label style={styles.label} htmlFor={`var-mode-${index}`}>Options</label>
                    <select
                      id={`var-mode-${index}`}
                      aria-label="Options mode"
                      style={styles.input}
                      value={row.mode}
                      onChange={(e) => updateRow(index, { mode: e.target.value as 'static' | 'query' })}
                    >
                      <option value="static">Static list</option>
                      <option value="query">Query</option>
                    </select>
                    {row.mode === 'static' ? (
                      <textarea
                        aria-label="Static options"
                        style={styles.textarea}
                        placeholder={'EMEA\nAMER\nAPAC'}
                        value={row.optionsText}
                        onChange={(e) => updateRow(index, { optionsText: e.target.value })}
                      />
                    ) : (
                      <>
                        <ConnectorSelector value={row.queryConnectorId} onChange={(id) => updateRow(index, { queryConnectorId: id })} />
                        <SqlEditor value={row.querySql} onChange={(sql) => updateRow(index, { querySql: sql })} minHeight={100} />
                        <div style={styles.grid2}>
                          <input
                            aria-label="Label column"
                            style={styles.input}
                            placeholder="label_column"
                            value={row.labelColumn}
                            onChange={(e) => updateRow(index, { labelColumn: e.target.value })}
                          />
                          <input
                            aria-label="Value column"
                            style={styles.input}
                            placeholder="value_column"
                            value={row.valueColumn}
                            onChange={(e) => updateRow(index, { valueColumn: e.target.value })}
                          />
                        </div>
                      </>
                    )}
                  </>
                )}

                <div style={styles.checkboxRow}>
                  <input
                    id={`var-required-${index}`}
                    aria-label="Required"
                    type="checkbox"
                    checked={row.required}
                    onChange={(e) => updateRow(index, { required: e.target.checked })}
                  />
                  <label htmlFor={`var-required-${index}`}>Required</label>
                </div>

                {otherNames.length > 0 && (
                  <div>
                    <label style={styles.label} htmlFor={`var-depends-${index}`}>Depends on</label>
                    <select
                      id={`var-depends-${index}`}
                      aria-label="Depends on"
                      multiple
                      style={{ ...styles.input, height: 'auto', minHeight: 56 }}
                      value={row.dependsOn}
                      onChange={(e) => updateRow(index, { dependsOn: Array.from(e.target.selectedOptions).map(o => o.value) })}
                    >
                      {otherNames.map(name => <option key={name} value={name}>{name}</option>)}
                    </select>
                  </div>
                )}
              </div>
            )
          })}

          <button
            type="button"
            style={styles.addBtn}
            onClick={() => { setSaved(false); setRows(prev => [...prev, emptyVariable()]) }}
          >
            <Plus size={12} /> Add variable
          </button>

          <div style={styles.checkboxRow}>
            <input
              id="public-live"
              aria-label="Public live queries"
              type="checkbox"
              checked={publicLive}
              onChange={(e) => { setSaved(false); setPublicLive(e.target.checked) }}
            />
            <label htmlFor="public-live">Public live queries</label>
          </div>
          <span style={styles.muted}>
            When enabled, public visitors run this dashboard's query widgets as you, rate-limited
            per visitor. Disabled by default.
          </span>

          {error && <div style={styles.error} role="alert">{error}</div>}
          {saved && <div style={styles.saved}>Variables saved</div>}

          <button type="button" style={styles.saveBtn} disabled={saving} onClick={save}>
            {saving ? 'Saving…' : 'Save variables'}
          </button>
        </div>
      </div>
    </>,
    document.body,
  )
}
