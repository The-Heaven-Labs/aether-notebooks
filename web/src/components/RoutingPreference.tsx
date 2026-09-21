import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { effectiveAccess, setPreference, type WarehouseServiceChoice } from '../api/warehouses'
import { ErrorBanner } from './ErrorBanner'
import { Modal } from './Modal'

interface RoutingPreferenceProps {
  warehouseId: string
  /**
   * Display name of the warehouse. Callers that cannot read warehouses (only
   * org admins can) fall back to the service names they do know.
   */
  warehouseName?: string
}

/**
 * Per-user service routing for one warehouse: pick which of the services the
 * user may `use` their queries run on, or clear the choice to route
 * automatically (a sole service, or a prompt when several are available).
 */
export function RoutingPreference({ warehouseId, warehouseName }: RoutingPreferenceProps) {
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const label = warehouseName ?? 'Warehouse'

  const { data, isLoading, isError } = useQuery({
    queryKey: ['warehouse-effective-access', warehouseId],
    queryFn: () => effectiveAccess(warehouseId),
  })

  const services = data?.services ?? []
  const preferred = data?.preferred_connector_id ?? ''

  const save = useMutation({
    mutationFn: (connectorId: string | null) => setPreference(warehouseId, connectorId),
    onSuccess: () => {
      setError(null)
      qc.invalidateQueries({ queryKey: ['warehouse-effective-access', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouse-grants', warehouseId] })
    },
    onError: (err: Error) => setError(err.message),
  })

  const handleChange = (value: string) => {
    save.mutate(value === '' ? null : value)
  }

  return (
    <section style={styles.section} aria-label={`Routing preference for ${label}`}>
      <div style={styles.info}>
        <div style={styles.name}>{label}</div>
        <div style={styles.hint}>
          {services.length === 0
            ? 'No service access: ask an admin to grant use on a service.'
            : services.length === 1
              ? 'Queries run on the only service you can use.'
              : 'Queries run on your preferred service unless a run is pinned.'}
        </div>
      </div>
      {isLoading ? (
        <span style={styles.muted}>Loading services…</span>
      ) : isError ? (
        <span style={styles.errorText}>Failed to load services</span>
      ) : (
        <select
          aria-label={`Preferred service for ${label}`}
          style={styles.select}
          value={preferred}
          disabled={services.length === 0 || save.isPending}
          onChange={(e) => handleChange(e.target.value)}
        >
          <option value="">
            {services.length === 0 ? 'No permitted services' : 'Automatic (choose when needed)'}
          </option>
          {services.map((service) => (
            <option key={service.connector_id} value={service.connector_id}>
              {service.name}
              {service.preferred ? ' (current)' : ''}
            </option>
          ))}
        </select>
      )}
      {save.isPending && <span style={styles.muted}>Saving…</span>}
      {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}
    </section>
  )
}

interface ServiceChoiceDialogProps {
  open: boolean
  services: WarehouseServiceChoice[]
  saving?: boolean
  error?: string | null
  onSelect: (connectorId: string) => void
  onCancel: () => void
}

/**
 * Prompt shown when an execution returns 409 service_choice_required: the
 * warehouse has several permitted services and no preference picked one.
 * Choosing a service stores it as the user's routing preference.
 */
export function ServiceChoiceDialog({
  open,
  services,
  saving = false,
  error = null,
  onSelect,
  onCancel,
}: ServiceChoiceDialogProps) {
  if (!open) return null

  return (
    <Modal title="Choose a warehouse service" onClose={onCancel} minWidth={360}>
      <div style={styles.dialogBody}>
        <p style={styles.dialogText}>
          More than one service in this warehouse is available to you. Pick the service
          this query should run on — the choice is saved as your routing preference.
        </p>
        {error && <ErrorBanner message={error} onDismiss={() => {}} />}
        <div style={styles.choiceList}>
          {services.map((service) => (
            <button
              key={service.connector_id}
              type="button"
              style={styles.choiceBtn}
              disabled={saving}
              onClick={() => onSelect(service.connector_id)}
            >
              {service.name}
            </button>
          ))}
        </div>
        {saving && <span style={styles.muted}>Saving preference…</span>}
      </div>
    </Modal>
  )
}

const styles: Record<string, React.CSSProperties> = {
  section: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
    flexWrap: 'wrap',
  },
  info: {
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    minWidth: 0,
  },
  name: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  hint: {
    fontSize: 11,
    color: 'var(--text-muted)',
  },
  muted: {
    fontSize: 12,
    color: 'var(--text-muted)',
  },
  errorText: {
    fontSize: 12,
    color: 'var(--error)',
  },
  select: {
    padding: '6px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    fontFamily: 'var(--font-mono)',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    maxWidth: 240,
  },
  dialogBody: {
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
    padding: 16,
  },
  dialogText: {
    margin: 0,
    fontSize: 13,
    color: 'var(--text-secondary)',
    lineHeight: 1.5,
  },
  choiceList: {
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  choiceBtn: {
    padding: '8px 12px',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
    cursor: 'pointer',
    textAlign: 'left',
  },
}
