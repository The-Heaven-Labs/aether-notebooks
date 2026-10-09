import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Boxes, ChevronRight, Pencil, Trash2 } from 'lucide-react'
import { AppShell } from '../components/AppShell'
import { api, ApiError } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { EmptyState } from '../components/EmptyState'
import { ErrorBanner } from '../components/ErrorBanner'
import { FormModal } from '../components/FormModal'
import { NewTablesInbox } from '../components/NewTablesInbox'
import { RowAction } from '../components/RowActions'
import { SectionHeader } from '../components/SectionHeader'
import { StatusBadge } from '../components/StatusBadge'
import { WarehouseHiddenTables } from '../components/WarehouseHiddenTables'
import { WarehouseTableGrants } from '../components/WarehouseTableGrants'
import {
  createWarehouse,
  deleteWarehouse,
  getWarehouse,
  getWarehouseValidation,
  listWarehouses,
  setConnectorWarehouse,
  setWarehouseProvisioner,
  updateWarehouse,
  type Warehouse,
  type WarehouseConnector,
  type WarehouseSyncStatus,
} from '../api/warehouses'
import type { Connector } from '../types'
import { useWarehouseTablePermissions } from '../hooks/useWarehouseTablePermissions'

export function WarehouseSettingsPage() {
  useEffect(() => { document.title = 'Warehouses — Aether Notebooks' }, [])
  const qc = useQueryClient()
  const tablePermissionsEnabled = useWarehouseTablePermissions()

  const [creating, setCreating] = useState(false)
  const [form, setForm] = useState({ name: '', provisioner_connector_id: '' })
  const [createError, setCreateError] = useState<string | null>(null)
  const [renameTarget, setRenameTarget] = useState<Warehouse | null>(null)
  const [renameName, setRenameName] = useState('')
  const [renameError, setRenameError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Warehouse | null>(null)
  const [forceTarget, setForceTarget] = useState<{ warehouse: Warehouse; message: string } | null>(null)
  const [unlinkTarget, setUnlinkTarget] = useState<{ warehouse: Warehouse; connector: WarehouseConnector } | null>(null)
  const [allowProvisionerSettled, setAllowProvisionerSettled] = useState(0)
  const createNameRef = useRef<HTMLInputElement>(null)
  const renameNameRef = useRef<HTMLInputElement>(null)

  const {
    data: warehouses = [],
    isLoading,
    isError: listFailed,
    error: listError,
  } = useQuery({
    queryKey: ['warehouses'],
    queryFn: listWarehouses,
    refetchInterval: (query) => {
      if (!tablePermissionsEnabled) return false
      const data = query.state.data
      if (data?.some((w) => w.sync_status === 'pending' || w.sync_status === 'syncing')) return 4000
      return false
    },
  })

  const { data: connectors = [] } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })

  const clickhouseConnectors = useMemo(
    () => connectors.filter((c) => c.type === 'clickhouse'),
    [connectors],
  )

  const invalidateWarehouses = () => {
    qc.invalidateQueries({ queryKey: ['warehouses'] })
    qc.invalidateQueries({ queryKey: ['connectors'] })
    qc.invalidateQueries({ queryKey: ['warehouse'] })
  }

  const closeCreate = () => {
    setCreating(false)
    setForm({ name: '', provisioner_connector_id: '' })
    setCreateError(null)
  }

  const closeRename = () => {
    setRenameTarget(null)
    setRenameName('')
    setRenameError(null)
  }

  const createMutation = useMutation({
    mutationFn: () =>
      createWarehouse({
        name: form.name.trim(),
        provisioner_connector_id: form.provisioner_connector_id || null,
      }),
    onSuccess: (warehouse) => {
      invalidateWarehouses()
      closeCreate()
      setExpandedId(warehouse.id)
    },
    onError: (err: Error) => setCreateError(err.message),
  })

  const renameMutation = useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) => updateWarehouse(id, { name }),
    onSuccess: () => {
      invalidateWarehouses()
      setError(null)
      closeRename()
    },
    onError: (err: Error) => setRenameError(err.message),
  })

  const allowProvisionerMutation = useMutation({
    mutationFn: ({ id, allow }: { id: string; allow: boolean }) =>
      updateWarehouse(id, { allow_provisioner_execution: allow }),
    onMutate: ({ id, allow }) => {
      const previous = qc.getQueryData<Warehouse>(['warehouse', id])
      void qc.cancelQueries({ queryKey: ['warehouse', id] })
      if (previous) {
        qc.setQueryData<Warehouse>(['warehouse', id], {
          ...previous,
          allow_provisioner_execution: allow,
        })
      }
      return { previous }
    },
    onError: (err: Error, { id }, context) => {
      if (context?.previous) {
        qc.setQueryData<Warehouse>(['warehouse', id], context.previous)
      }
      setAllowProvisionerSettled((n) => n + 1)
      setError(err.message)
    },
    onSuccess: () => {
      setAllowProvisionerSettled((n) => n + 1)
      invalidateWarehouses()
      setError(null)
    },
  })

  const provisionerMutation = useMutation({
    mutationFn: ({ id, connectorId }: { id: string; connectorId: string | null }) =>
      setWarehouseProvisioner(id, connectorId),
    onSuccess: () => {
      invalidateWarehouses()
      setError(null)
    },
    onError: (err: Error) => setError(err.message),
  })

  const linkMutation = useMutation({
    mutationFn: ({ connectorId, warehouseId }: { connectorId: string; warehouseId: string | null }) =>
      setConnectorWarehouse(connectorId, warehouseId),
    onSuccess: () => {
      invalidateWarehouses()
      setError(null)
    },
    onError: (err: Error) => setError(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: ({ warehouse, force }: { warehouse: Warehouse; force: boolean }) =>
      deleteWarehouse(warehouse.id, force),
    onSuccess: () => {
      invalidateWarehouses()
      setDeleteTarget(null)
      setForceTarget(null)
      setError(null)
    },
    onError: (err: Error, { warehouse, force }) => {
      if (!force && err instanceof ApiError && err.status === 409) {
        setDeleteTarget(null)
        setForceTarget({ warehouse, message: err.message })
        return
      }
      setError(err.message)
    },
  })

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader
          title="Warehouses"
          subtitle={
            warehouses.length > 0
              ? `${warehouses.length} warehouse${warehouses.length !== 1 ? 's' : ''}`
              : ''
          }
        >
          <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>
            + New Warehouse
          </button>
        </SectionHeader>
        <p style={styles.intro}>
          {tablePermissionsEnabled ? (
            <>
              Group ClickHouse connectors that share one access namespace. Linked connectors run as
              per-user identities with table grants enforced by ClickHouse; unlinked connectors keep
              using their shared credential.
            </>
          ) : (
            <>
              Group ClickHouse connectors that share one access namespace. ClickHouse table
              permissions are currently disabled, so linked connectors execute with their stored
              credential; per-user identities and table grants begin once an operator enables
              AETHER_CH_TABLE_PERMISSIONS.
            </>
          )}
        </p>

        {creating && (
          <FormModal
            title="New Warehouse"
            onClose={closeCreate}
            initialFocusRef={createNameRef}
            width="min(560px, 92vw)"
            error={createError}
            submitLabel="Create"
            pendingLabel="Creating…"
            pending={createMutation.isPending}
            submitDisabled={!form.name.trim()}
            onSubmit={() => createMutation.mutate()}
          >
            <label style={styles.label}>
              Name
              <input
                ref={createNameRef}
                style={styles.input}
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                placeholder="Analytics Warehouse"
              />
            </label>
            <label style={styles.label}>
              Provisioner connector
              <select
                style={styles.input}
                value={form.provisioner_connector_id}
                onChange={(e) => setForm((f) => ({ ...f, provisioner_connector_id: e.target.value }))}
              >
                <option value="">— None (choose later) —</option>
                {clickhouseConnectors
                  .filter((c) => !c.warehouse_id)
                  .map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
              </select>
            </label>
          </FormModal>
        )}

        {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}
        {listFailed && (
          <ErrorBanner
            message={listError instanceof Error ? listError.message : 'Failed to load warehouses'}
          />
        )}

        {isLoading ? (
          <div style={styles.loading}>Loading warehouses…</div>
        ) : warehouses.length === 0 && !listFailed ? (
          <EmptyState
            icon={<Boxes size={28} />}
            title="No warehouses yet"
            text={tablePermissionsEnabled
              ? 'Create a warehouse to group ClickHouse services and enforce per-table grants.'
              : 'Create a warehouse to group ClickHouse services for per-user table grants.'}
            action={{ label: '+ New Warehouse', onClick: () => setCreating(true) }}
          />
        ) : (
          <div style={styles.list}>
            {warehouses.map((warehouse) => (
              <WarehouseCard
                key={warehouse.id}
                warehouse={warehouse}
                clickhouseConnectors={clickhouseConnectors}
                expanded={expandedId === warehouse.id}
                onToggle={() => setExpandedId(expandedId === warehouse.id ? null : warehouse.id)}
                onRequestRename={() => { setRenameTarget(warehouse); setRenameName(warehouse.name); setRenameError(null) }}
                onDelete={() => setDeleteTarget(warehouse)}
                onSetProvisioner={(connectorId) =>
                  provisionerMutation.mutate({ id: warehouse.id, connectorId })
                }
                onSetAllowProvisionerExecution={(allow) =>
                  allowProvisionerMutation.mutate({ id: warehouse.id, allow })
                }
                allowProvisionerPending={allowProvisionerMutation.isPending}
                allowProvisionerSettled={allowProvisionerSettled}
                onAddConnector={(connectorId) =>
                  linkMutation.mutate({ connectorId, warehouseId: warehouse.id })
                }
                onRemoveConnector={(connector) =>
                  setUnlinkTarget({ warehouse, connector })
                }
              />
            ))}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete warehouse"
        message={tablePermissionsEnabled
          ? `Delete "${deleteTarget?.name}"? Linked connectors return to shared-credential mode and managed ClickHouse identities are revoked.`
          : `Delete "${deleteTarget?.name}"? Linked connectors return to shared-credential mode; ClickHouse table permissions are disabled, so any provisioned identities are left in place.`}
        confirmLabel="Delete"
        destructive
        confirmDisabled={deleteMutation.isPending}
        onConfirm={() => {
          if (deleteTarget) deleteMutation.mutate({ warehouse: deleteTarget, force: false })
          setDeleteTarget(null)
        }}
        onCancel={() => setDeleteTarget(null)}
      />

      <ConfirmDialog
        open={!!forceTarget}
        title="Warehouse has linked connectors"
        message={forceTarget?.message}
        confirmLabel="Delete and unlink"
        destructive
        confirmDisabled={deleteMutation.isPending}
        onConfirm={() => {
          if (forceTarget) deleteMutation.mutate({ warehouse: forceTarget.warehouse, force: true })
        }}
        onCancel={() => setForceTarget(null)}
      />

      <ConfirmDialog
        open={!!unlinkTarget}
        title="Unlink connector"
        message={tablePermissionsEnabled
          ? `Unlink "${unlinkTarget?.connector.name}" from "${unlinkTarget?.warehouse.name}"? It returns to shared-credential mode and table grants no longer apply.`
          : `Unlink "${unlinkTarget?.connector.name}" from "${unlinkTarget?.warehouse.name}"? It returns to shared-credential mode.`}
        confirmLabel="Unlink"
        destructive
        onConfirm={() => {
          if (unlinkTarget) {
            linkMutation.mutate({ connectorId: unlinkTarget.connector.id, warehouseId: null })
          }
          setUnlinkTarget(null)
        }}
        onCancel={() => setUnlinkTarget(null)}
      />

      {renameTarget && (
        <FormModal
          title={`Rename "${renameTarget.name}"`}
          onClose={closeRename}
          initialFocusRef={renameNameRef}
          width="min(420px, 92vw)"
          error={renameError}
          submitLabel="Save"
          pendingLabel="Saving…"
          pending={renameMutation.isPending}
          submitDisabled={!renameName.trim() || renameName.trim() === renameTarget.name}
          onSubmit={() => renameMutation.mutate({ id: renameTarget.id, name: renameName.trim() })}
        >
          <label style={styles.label}>
            Name
            <input
              ref={renameNameRef}
              style={styles.input}
              value={renameName}
              onChange={(e) => setRenameName(e.target.value)}
            />
          </label>
        </FormModal>
      )}
    </AppShell>
  )
}

interface WarehouseCardProps {
  warehouse: Warehouse
  clickhouseConnectors: Connector[]
  expanded: boolean
  onToggle: () => void
  onRequestRename: () => void
  onDelete: () => void
  onSetProvisioner: (connectorId: string | null) => void
  onSetAllowProvisionerExecution: (allow: boolean) => void
  allowProvisionerPending: boolean
  allowProvisionerSettled: number
  onAddConnector: (connectorId: string) => void
  onRemoveConnector: (connector: WarehouseConnector) => void
}

function WarehouseCard({
  warehouse,
  clickhouseConnectors,
  expanded,
  onToggle,
  onRequestRename,
  onDelete,
  onSetProvisioner,
  onSetAllowProvisionerExecution,
  allowProvisionerPending,
  allowProvisionerSettled,
  onAddConnector,
  onRemoveConnector,
}: WarehouseCardProps) {
  const [addConnectorId, setAddConnectorId] = useState('')
  const tablePermissionsEnabled = useWarehouseTablePermissions()

  const { data: detail, isLoading } = useQuery({
    queryKey: ['warehouse', warehouse.id],
    queryFn: () => getWarehouse(warehouse.id),
    enabled: expanded,
  })

  // Shares the new-tables inbox's query: expanding the card is what loads
  // validation, so the header hint never triggers extra fetching.
  const { data: validation } = useQuery({
    queryKey: ['warehouse-validation', warehouse.id],
    queryFn: () => getWarehouseValidation(warehouse.id),
    enabled: expanded,
  })

  const serverAllowProvisioner =
    detail?.allow_provisioner_execution ?? warehouse.allow_provisioner_execution ?? false
  const [allowProvisionerOverride, setAllowProvisionerOverride] = useState<boolean | null>(null)
  useEffect(() => {
    setAllowProvisionerOverride(null)
  }, [serverAllowProvisioner, allowProvisionerSettled])
  const allowProvisionerChecked = allowProvisionerOverride ?? serverAllowProvisioner

  const validationWarnings = validation
    ? validation.tables_without_service_access.length + validation.service_access_without_tables.length
    : 0

  const linked = detail?.connectors ?? []
  const hiddenPatterns = detail?.hidden_table_patterns ?? warehouse.hidden_table_patterns ?? []
  const unlinked = clickhouseConnectors.filter((c) => !c.warehouse_id)

  return (
    <div style={styles.card}>
      <div style={styles.cardHeader}>
        <button
          type="button"
          style={{ ...styles.expandBtn, flex: 1 }}
          onClick={onToggle}
          aria-expanded={expanded}
          title={expanded ? 'Collapse' : 'Expand'}
        >
          <ChevronRight
            size={14}
            style={{ ...styles.chevron, transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}
          />
          <span style={styles.warehouseName}>{warehouse.name}</span>
        </button>
        <span style={styles.syncWrap}>
          <SyncBadge status={warehouse.sync_status} title={syncStatusTitle(warehouse)} />
          {validationWarnings > 0 && (
            <span
              style={styles.warningsHint}
              title={`${validationWarnings} validation warning${validationWarnings !== 1 ? 's' : ''}: some subjects are missing table grants or service access.`}
            >
              with warnings
            </span>
          )}
        </span>
        <div style={styles.actions}>
          <RowAction label="Rename warehouse" icon={<Pencil size={13} />} accent onClick={onRequestRename} />
          <RowAction label="Delete warehouse" icon={<Trash2 size={13} />} danger onClick={onDelete} />
        </div>
      </div>

      {expanded && (
        <div style={styles.cardBody}>
          {warehouse.sync_status === 'error' && warehouse.sync_error && (
            <div style={styles.syncError}>{warehouse.sync_error}</div>
          )}

          {isLoading ? (
            <div style={styles.loading}>Loading warehouse…</div>
          ) : (
            <>
              <div style={styles.field}>
                <label style={styles.label} htmlFor={`provisioner-${warehouse.id}`}>
                  Provisioner connector
                </label>
                <select
                  id={`provisioner-${warehouse.id}`}
                  aria-label="Provisioner connector"
                  style={{ ...styles.input, maxWidth: 320 }}
                  value={detail?.provisioner_connector_id ?? ''}
                  onChange={(e) => onSetProvisioner(e.target.value || null)}
                >
                  <option value="">— None —</option>
                  {linked.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
                <span style={styles.hint}>
                  {tablePermissionsEnabled
                    ? 'Runs the DDL that provisions per-user ClickHouse access.'
                    : 'ClickHouse table permissions are disabled; linked services keep using the stored credential. The provisioner never runs user queries while they are disabled.'}
                </span>
              </div>

              <div style={styles.field}>
                <label style={{ ...styles.label, display: 'flex', flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                  <input
                    type="checkbox"
                    aria-label="Allow queries through the provisioner"
                    checked={allowProvisionerChecked}
                    disabled={!detail?.provisioner_connector_id || allowProvisionerPending}
                    aria-describedby={`allow-provisioner-hint-${warehouse.id}`}
                    onChange={(e) => {
                      setAllowProvisionerOverride(e.target.checked)
                      onSetAllowProvisionerExecution(e.target.checked)
                    }}
                  />
                  Allow queries through the provisioner
                </label>
                <span id={`allow-provisioner-hint-${warehouse.id}`} style={styles.hint}>
                  Off by default. The provisioner credential is reserved for background provisioning;
                  when off, notebook and agent queries cannot run through this connector.
                  {!tablePermissionsEnabled && ' Takes effect once ClickHouse table permissions are enabled.'}
                </span>
              </div>

              <div style={styles.services}>
                <div style={styles.servicesHeader}>Connectors</div>
                {linked.length === 0 && (
                  <div style={styles.emptyLine}>No connectors linked to this warehouse.</div>
                )}
                {linked.map((c) => (
                  <div key={c.id} style={styles.serviceRow}>
                    <span style={styles.serviceName}>{c.name}</span>
                    {c.is_provisioner && <span style={styles.provisionerBadge}>Provisioner</span>}
                    <span style={styles.serviceType}>{c.type}</span>
                    <button
                      type="button"
                      style={styles.removeBtn}
                      title="Unlink connector"
                      onClick={() => onRemoveConnector(c)}
                    >
                      Remove
                    </button>
                  </div>
                ))}
                <div style={styles.addConnectorRow}>
                  <select
                    aria-label="Add connector"
                    style={{ ...styles.input, maxWidth: 320 }}
                    value={addConnectorId}
                    onChange={(e) => setAddConnectorId(e.target.value)}
                  >
                    <option value="">
                      {unlinked.length === 0 ? 'No unlinked ClickHouse connectors' : 'Select connector…'}
                    </option>
                    {unlinked.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.name}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    style={{
                      ...styles.addBtn,
                      opacity: addConnectorId ? 1 : 0.5,
                      cursor: addConnectorId ? 'pointer' : 'not-allowed',
                    }}
                    disabled={!addConnectorId}
                    onClick={() => {
                      onAddConnector(addConnectorId)
                      setAddConnectorId('')
                    }}
                  >
                    Add
                  </button>
                </div>
              </div>

              <WarehouseHiddenTables
                warehouseId={warehouse.id}
                patterns={hiddenPatterns}
                connectors={linked}
              />
              <WarehouseTableGrants warehouseId={warehouse.id} connectors={linked} />
              <NewTablesInbox warehouseId={warehouse.id} />
            </>
          )}
        </div>
      )}
    </div>
  )
}

function syncStatusTitle(warehouse: Warehouse): string {
  switch (warehouse.sync_status) {
    case 'ready':
      return 'Provisioning succeeded: per-user ClickHouse identities are in place. Table access still depends on each subject\'s table grants and service access.'
    case 'error':
      return warehouse.sync_error ?? 'Sync failed'
    case 'syncing':
      return 'Reconciling ClickHouse identities and table grants…'
    default:
      return 'Waiting to reconcile ClickHouse identities and table grants.'
  }
}

function SyncBadge({ status, title }: { status: WarehouseSyncStatus; title?: string }) {
  switch (status) {
    case 'ready':
      return <StatusBadge status="success" label="Ready" title={title} />
    case 'error':
      return <StatusBadge status="error" label="Error" title={title} />
    case 'syncing':
      return <StatusBadge status="neutral" label="Syncing…" title={title} />
    default:
      return <StatusBadge status="neutral" label="Pending" title={title} />
  }
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1100, margin: '0 auto', padding: 'clamp(16px, 4vw, 32px)', width: '100%' },
  intro: { fontSize: 13, color: 'var(--text-muted)', marginTop: -16, marginBottom: 24, maxWidth: 720, lineHeight: 1.5 },
  newBtn: { padding: '7px 16px', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  list: { display: 'flex', flexDirection: 'column', gap: 8 },
  card: { border: '1px solid var(--border)', borderRadius: 4, background: 'var(--bg-card)' },
  cardHeader: { display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px' },
  syncWrap: { display: 'inline-flex', alignItems: 'center', gap: 6, flexShrink: 0 },
  warningsHint: { fontSize: 10, fontWeight: 700, color: 'var(--warning-text)', background: 'var(--warning-light)', border: '1px solid var(--warning-border)', borderRadius: 10, padding: '1px 8px', textTransform: 'uppercase' as const, letterSpacing: '0.05em', whiteSpace: 'nowrap' as const },
  expandBtn: { display: 'flex', alignItems: 'center', gap: 8, background: 'transparent', border: 'none', cursor: 'pointer', padding: 0, flex: 1, textAlign: 'left' as const, minWidth: 0 },
  chevron: { color: 'var(--text-secondary)', transition: 'transform 0.15s ease', flexShrink: 0 },
  warehouseName: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' },
  actions: { display: 'flex', gap: 6, flexShrink: 0 },
  cardBody: { borderTop: '1px solid var(--border)', padding: '14px 16px', background: 'var(--bg-secondary)', display: 'flex', flexDirection: 'column', gap: 14 },
  syncError: { fontSize: 12, fontFamily: 'var(--font-mono)', color: 'var(--error-full)', background: 'var(--error-light)', border: '1px solid var(--error-border)', borderRadius: 4, padding: '6px 10px', whiteSpace: 'pre-wrap' },
  field: { display: 'flex', flexDirection: 'column', gap: 4 },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  hint: { fontSize: 11, color: 'var(--text-muted)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)' },
  services: { display: 'flex', flexDirection: 'column', gap: 6 },
  servicesHeader: { fontSize: 12, fontWeight: 700, color: 'var(--text-secondary)', textTransform: 'uppercase' as const, letterSpacing: '0.06em' },
  serviceRow: { display: 'flex', alignItems: 'center', gap: 8, padding: '5px 0', borderBottom: '1px solid var(--border-light)' },
  serviceName: { fontSize: 13, fontWeight: 500, color: 'var(--text-primary)' },
  serviceType: { fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' },
  provisionerBadge: { fontSize: 10, fontWeight: 700, color: 'var(--accent)', background: 'var(--accent-light)', border: '1px solid var(--border)', borderRadius: 10, padding: '1px 8px', textTransform: 'uppercase' as const, letterSpacing: '0.05em' },
  removeBtn: { marginLeft: 'auto', padding: '2px 8px', fontSize: 11, border: '1px solid var(--border)', borderRadius: 4, background: 'transparent', cursor: 'pointer', color: 'var(--error)' },
  addConnectorRow: { display: 'flex', gap: 8, alignItems: 'center', marginTop: 4 },
  addBtn: { padding: '6px 14px', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none', borderRadius: 4, fontSize: 12, fontWeight: 600 },
  emptyLine: { fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  loading: { fontSize: 13, color: 'var(--text-muted)', padding: '4px 0' },
}
