import { useState } from 'react'
import type { Parameter } from '../types'
import { Settings, X } from 'lucide-react'

interface Props {
  parameters: Parameter[]
  values: Record<string, string>
  onChange: (values: Record<string, string>) => void
  onSaveDefinitions: (params: Parameter[]) => void
}

export function ParametersBar({ parameters, values, onChange, onSaveDefinitions }: Props) {
  const [managing, setManaging] = useState(false)
  const [draftParams, setDraftParams] = useState<Parameter[]>(parameters)

  if (parameters.length === 0 && !managing) {
    return (
      <div style={styles.bar}>
        <button style={{ ...styles.manageBtn, display: 'flex', alignItems: 'center', gap: 4 }} onClick={() => { setDraftParams([]); setManaging(true) }}>
          <Settings size={13} /> Add Parameters
        </button>
      </div>
    )
  }

  return (
    <div style={styles.bar}>
      {!managing && (
        <div style={styles.paramsList}>
          {parameters.map((p) => (
            <label key={p.name} style={styles.paramField}>
              <span style={styles.paramName}>{p.name}</span>
              <input
                style={styles.paramInput}
                value={values[p.name] ?? p.default}
                onChange={(e) => onChange({ ...values, [p.name]: e.target.value })}
                placeholder={p.default || p.name}
              />
            </label>
          ))}
          <button style={{ ...styles.manageBtn, display: 'flex', alignItems: 'center' }} onClick={() => { setDraftParams(parameters); setManaging(true) }}>
            <Settings size={13} />
          </button>
        </div>
      )}
      {managing && (
        <div style={styles.managePanel}>
          <span style={styles.manageTitle}>Parameters</span>
          {draftParams.map((p, i) => (
            <div key={p.name || i} style={styles.draftRow}>
              <input
                style={styles.draftInput}
                placeholder="name"
                value={p.name}
                onChange={(e) => {
                  const next = [...draftParams]
                  next[i] = { ...next[i], name: e.target.value }
                  setDraftParams(next)
                }}
              />
              <select
                style={styles.draftInput}
                value={p.type}
                onChange={(e) => {
                  const next = [...draftParams]
                  next[i] = { ...next[i], type: e.target.value as Parameter['type'] }
                  setDraftParams(next)
                }}
              >
                <option value="string">string</option>
                <option value="number">number</option>
                <option value="boolean">boolean</option>
                <option value="date">date</option>
                <option value="daterange">daterange</option>
              </select>
              <input
                style={styles.draftInput}
                placeholder="default"
                value={p.default}
                onChange={(e) => {
                  const next = [...draftParams]
                  next[i] = { ...next[i], default: e.target.value }
                  setDraftParams(next)
                }}
              />
              <button
                style={{ ...styles.removeBtn, display: 'flex', alignItems: 'center' }}
                onClick={() => setDraftParams(draftParams.filter((_, j) => j !== i))}
              ><X size={13} /></button>
            </div>
          ))}
          <div style={styles.manageActions}>
            <button
              style={styles.addParamBtn}
              onClick={() => setDraftParams([...draftParams, { name: '', type: 'string', default: '' }])}
            >
              + Add
            </button>
            <button style={styles.cancelBtn} onClick={() => setManaging(false)}>Cancel</button>
            <button
              style={styles.saveBtn}
              onClick={() => { onSaveDefinitions(draftParams); setManaging(false) }}
            >
              Save
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  bar: {
    background: 'var(--bg-secondary)',
    borderBottom: '1px solid var(--border-light)',
    padding: '6px 40px',
    display: 'flex',
    alignItems: 'center',
    flexWrap: 'wrap',
    gap: 8,
    minHeight: 40,
  },
  paramsList: { display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap', flex: 1 },
  paramField: { display: 'flex', alignItems: 'center', gap: 5, fontSize: 12 },
  paramName: { fontFamily: 'var(--font-mono)', fontWeight: 700, color: 'var(--text-secondary)', fontSize: 11 },
  paramInput: {
    padding: '2px 7px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    background: 'white',
    width: 110,
  },
  manageBtn: {
    padding: '3px 8px',
    fontSize: 11,
    fontWeight: 600,
    background: 'var(--border)',
    border: '1px solid transparent',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-muted)',
  },
  managePanel: { display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', width: '100%' },
  manageTitle: { fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', letterSpacing: '0.05em', textTransform: 'uppercase' },
  draftRow: { display: 'flex', gap: 5, alignItems: 'center' },
  draftInput: {
    padding: '3px 7px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    background: 'white',
  },
  removeBtn: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 13,
    padding: '0 3px',
  },
  manageActions: { display: 'flex', gap: 6, marginLeft: 'auto' },
  addParamBtn: {
    padding: '3px 10px',
    fontSize: 11,
    fontWeight: 600,
    border: '1.5px dashed var(--border)',
    borderRadius: 4,
    background: 'transparent',
    cursor: 'pointer',
    color: 'var(--text-muted)',
  },
  cancelBtn: {
    padding: '3px 10px',
    fontSize: 11,
    background: 'transparent',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
  saveBtn: {
    padding: '3px 10px',
    fontSize: 11,
    fontWeight: 600,
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 4,
    cursor: 'pointer',
  },
}
