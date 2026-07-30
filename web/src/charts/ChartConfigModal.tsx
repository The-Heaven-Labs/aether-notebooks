import { useState, useEffect, useCallback, useRef } from 'react'
import type React from 'react'
import { X, Save } from 'lucide-react'
import type { ChartConfig } from './types'
import { CHART_MODULES } from './registry'
import { ConfirmModal } from '../components/ConfirmModal'

interface ChartConfigModalProps {
  config: ChartConfig
  columns: string[]
  data?: { columns: { name: string; type?: string }[]; rows: any[][] }
  groupValues: string[]
  onSave: (config: ChartConfig) => void
  onClose: () => void
}

export function ChartConfigModal({ config, columns, data, groupValues, onSave, onClose }: ChartConfigModalProps) {
  const [workingConfig, setWorkingConfig] = useState<ChartConfig>(() => {
    const cloned = deepCloneConfig(config)
    // Auto-detect column mappings on first open if config has no explicit columns set
    if (!cloned.xAxis && (!cloned.yAxis || cloned.yAxis.length === 0) && columns.length > 0 && data) {
      const mod = CHART_MODULES[cloned.chartType ?? 'bar']
      if (mod?.detectColumns) {
        const detected = mod.detectColumns(
          data.columns.map(c => ({ name: c.name, type: c.type ?? '' })),
          data.rows,
        )
        return { ...cloned, ...detected }
      }
    }
    return cloned
  })
  const [showConfirm, setShowConfirm] = useState(false)
  const dirtyRef = useRef(false)

  const markDirty = useCallback(() => {
    dirtyRef.current = true
  }, [])

  const chartType = workingConfig.chartType ?? 'bar'
  const prevChartTypeRef = useRef(chartType)
  const currentMod = CHART_MODULES[chartType]
  const effectiveConfig: ChartConfig = currentMod ? { ...currentMod.defaultConfig, ...workingConfig } : workingConfig

  const handleChange = useCallback((newCfg: ChartConfig) => {
    const newType = newCfg.chartType ?? 'bar'
    const prevType = prevChartTypeRef.current
    prevChartTypeRef.current = newType
    // When chart type changes, auto-detect column mappings for the new module
    if (newType !== prevType && columns.length > 0 && data) {
      const newMod = CHART_MODULES[newType]
      if (newMod?.detectColumns) {
        const detected = newMod.detectColumns(
          data.columns.map(c => ({ name: c.name, type: c.type ?? '' })),
          data.rows,
        )
        newCfg = { ...newCfg, ...detected }
      }
    }
    setWorkingConfig(newCfg)
    markDirty()
  }, [markDirty, columns, data])

  const handleSave = useCallback(() => {
    onSave(workingConfig)
    dirtyRef.current = false
    onClose()
  }, [workingConfig, onSave, onClose])

  const handleCancel = useCallback(() => {
    if (dirtyRef.current && !showConfirm) {
      setShowConfirm(true)
      return
    }
    onClose()
  }, [showConfirm, onClose])

  const handleConfirmDiscard = useCallback(() => {
    setShowConfirm(false)
    onClose()
  }, [onClose])

  const handleCancelDiscard = useCallback(() => {
    setShowConfirm(false)
  }, [])

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        handleCancel()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [handleCancel])

  return (
    <>
      {showConfirm && (
        <ConfirmModal
          title="Unsaved changes"
          message="You have unsaved chart configuration changes. Discard them?"
          confirmLabel="Discard changes"
          cancelLabel="Keep editing"
          onConfirm={handleConfirmDiscard}
          onCancel={handleCancelDiscard}
          destructive
        />
      )}
      <div style={styles.overlay} onClick={handleCancel}>
        <div style={styles.modal} onClick={e => e.stopPropagation()}>
          <div style={styles.header}>
            <span style={styles.headerTitle}>Chart Configuration</span>
            <div style={styles.headerActions}>
              <button style={styles.saveBtn} onClick={handleSave} title="Save changes">
                <Save size={14} /> Save
              </button>
              <button style={styles.cancelBtn} onClick={handleCancel} title="Cancel">
                <X size={16} />
              </button>
            </div>
          </div>
          <div style={styles.body}>
            <div style={styles.preview}>
              {currentMod ? (
                <currentMod.Component data={data as any} config={effectiveConfig} />
              ) : (
                <div style={{ color: 'var(--text-muted)', padding: 16 }}>Unknown chart type</div>
              )}
            </div>
            <div style={styles.configPanel}>
              {currentMod && (
                <currentMod.ConfigPanel
                  config={workingConfig}
                  columns={columns}
                  data={data}
                  groupValues={groupValues}
                  onChange={handleChange}
                />
              )}
            </div>
          </div>
        </div>
      </div>
    </>
  )
}

function deepCloneConfig(cfg: ChartConfig): ChartConfig {
  return JSON.parse(JSON.stringify(cfg))
}

const styles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)',
    zIndex: 1600, display: 'flex', alignItems: 'center', justifyContent: 'center',
    padding: 24,
  },
  modal: {
    display: 'flex', flexDirection: 'column',
    width: '90vw', maxWidth: 1400, height: '85vh',
    background: 'var(--bg-primary)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    overflow: 'hidden',
    boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
  },
  header: {
    display: 'flex', alignItems: 'center', justifyContent: 'space-between',
    padding: '10px 16px',
    borderBottom: '1px solid var(--border)',
    background: 'var(--bg-secondary)',
    flexShrink: 0,
  },
  headerTitle: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' },
  headerActions: { display: 'flex', alignItems: 'center', gap: 8 },
  saveBtn: {
    display: 'inline-flex', alignItems: 'center', gap: 4,
    fontSize: 12, padding: '5px 12px',
    background: 'var(--accent)', color: '#fff',
    border: 'none', borderRadius: 4, cursor: 'pointer', fontWeight: 500,
  },
  cancelBtn: {
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    width: 28, height: 28,
    background: 'transparent', color: 'var(--text-muted)',
    border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer',
  },
  body: {
    display: 'flex', flex: 1, overflow: 'hidden',
  },
  preview: {
    flex: 1, padding: 16, overflow: 'auto',
    display: 'flex', flexDirection: 'column',
    borderRight: '1px solid var(--border)',
  },
  configPanel: {
    width: 420, overflowY: 'auto', flexShrink: 0,
  },
}
