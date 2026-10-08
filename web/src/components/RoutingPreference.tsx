import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { effectiveAccess, setPreference } from '../api/warehouses'
import { ErrorBanner } from './ErrorBanner'

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
 * user may `use` their queries run on. The choice acts as the default service
 * for new notebook and dashboard selections; clearing it leaves them without
 * a default.
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
              : 'Used as the default service for new notebooks and dashboard selections.'}
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
            {services.length === 0 ? 'No permitted services' : 'No default'}
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
    // Wide enough for long service names without clipping.
    maxWidth: 320,
  },
}
