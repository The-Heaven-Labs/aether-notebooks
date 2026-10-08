import { api } from './client'

export type WarehouseSyncStatus = 'pending' | 'syncing' | 'ready' | 'error'

export type WarehouseSubjectType = 'user' | 'group' | 'everyone'

export interface WarehouseConnector {
  id: string
  name: string
  type: string
  is_provisioner: boolean
}

export interface Warehouse {
  id: string
  org_id: string
  name: string
  provisioner_connector_id: string | null
  allow_provisioner_execution: boolean
  hidden_table_patterns: string[]
  sync_status: WarehouseSyncStatus
  sync_error: string | null
  last_synced_at: string | null
  created_at: string
  updated_at: string
  /** Present only on GET /warehouses/{id}. */
  connectors?: WarehouseConnector[]
}

export interface WarehouseGrant {
  id: string
  org_id: string
  warehouse_id: string
  subject_type: WarehouseSubjectType
  subject_id: string
  subject_name?: string
  subject_email?: string
  database: string
  table: string
  created_by: string | null
  created_at: string
}

export interface WarehouseGrantCreateResult extends WarehouseGrant {
  /** Advisory state report; currently only "no_service_access". */
  warning?: string
}

export interface WarehouseEffectiveTable {
  database: string
  table: string
}

export interface WarehouseEffectiveService {
  connector_id: string
  name: string
  preferred: boolean
}

/**
 * One service offered by the routing preference picker: a warehouse service
 * the user may `use`, selectable as their default execution target.
 */
export interface WarehouseServiceChoice {
  connector_id: string
  name: string
}

export interface WarehouseEffectiveAccess {
  user_id: string
  warehouse_id: string
  ch_user?: string
  ch_roles?: string[]
  tables: WarehouseEffectiveTable[]
  services: WarehouseEffectiveService[]
  preferred_connector_id: string | null
}

/** One catalog table first observed since the warehouse's last grant review. */
export interface WarehouseNewTable {
  database: string
  table: string
  first_seen_at: string
}

export interface WarehouseNewTables {
  warehouse_id: string
  /** Cutoff applied: `since` when passed, otherwise the last grant review. */
  since: string
  /** True when the response stopped at the server's new-tables cap. */
  truncated: boolean
  tables: WarehouseNewTable[]
}

/** One subject in a validation warning list. */
export interface WarehouseValidationSubject {
  subject_type: WarehouseSubjectType
  subject_id: string
  subject_name?: string
  subject_email?: string
  /** "db.table" entries; set for granted-but-unusable subjects. */
  tables?: string[]
  /** Service names; set for subjects that can connect but hold no grants. */
  services?: string[]
}

export interface WarehouseValidation {
  warehouse_id: string
  truncated: boolean
  tables_without_service_access: WarehouseValidationSubject[]
  service_access_without_tables: WarehouseValidationSubject[]
}

export function listWarehouses(): Promise<Warehouse[]> {
  return api.get<Warehouse[]>('/api/v1/warehouses')
}

export function getWarehouse(id: string): Promise<Warehouse> {
  return api.get<Warehouse>(`/api/v1/warehouses/${id}`)
}

export function createWarehouse(data: {
  name: string
  provisioner_connector_id?: string | null
}): Promise<Warehouse> {
  return api.post<Warehouse>('/api/v1/warehouses', data)
}

export function updateWarehouse(
  id: string,
  data: {
    name?: string
    provisioner_connector_id?: string | null
    allow_provisioner_execution?: boolean
    hidden_table_patterns?: string[] | null
  },
): Promise<Warehouse> {
  return api.put<Warehouse>(`/api/v1/warehouses/${id}`, data)
}

export function deleteWarehouse(id: string, force = false): Promise<void> {
  const suffix = force ? '?force=true' : ''
  return api.delete<void>(`/api/v1/warehouses/${id}${suffix}`)
}

export function setWarehouseProvisioner(
  id: string,
  connectorId: string | null,
): Promise<Warehouse> {
  return api.put<Warehouse>(`/api/v1/warehouses/${id}/provisioner`, {
    connector_id: connectorId,
  })
}

export function setConnectorWarehouse(
  connectorId: string,
  warehouseId: string | null,
): Promise<{ id: string; warehouse_id: string | null }> {
  return api.put<{ id: string; warehouse_id: string | null }>(
    `/api/v1/connectors/${connectorId}/warehouse`,
    { warehouse_id: warehouseId },
  )
}

export function listGrants(warehouseId: string): Promise<WarehouseGrant[]> {
  return api.get<WarehouseGrant[]>(`/api/v1/warehouses/${warehouseId}/grants`)
}

export function createGrant(
  warehouseId: string,
  data: {
    subject_type: WarehouseSubjectType
    subject_id: string
    database: string
    table: string
  },
): Promise<WarehouseGrantCreateResult> {
  return api.post<WarehouseGrantCreateResult>(
    `/api/v1/warehouses/${warehouseId}/grants`,
    data,
  )
}

export function deleteGrant(warehouseId: string, grantId: string): Promise<void> {
  return api.delete<void>(`/api/v1/warehouses/${warehouseId}/grants/${grantId}`)
}

export function effectiveAccess(
  warehouseId: string,
  userId?: string,
): Promise<WarehouseEffectiveAccess> {
  const query = userId ? `?user_id=${encodeURIComponent(userId)}` : ''
  return api.get<WarehouseEffectiveAccess>(
    `/api/v1/warehouses/${warehouseId}/effective-access${query}`,
  )
}

export function setPreference(
  warehouseId: string,
  connectorId: string | null,
): Promise<{ user_id: string; warehouse_id: string; connector_id: string | null }> {
  return api.put<{ user_id: string; warehouse_id: string; connector_id: string | null }>(
    `/api/v1/warehouses/${warehouseId}/preference`,
    { connector_id: connectorId },
  )
}

export function listNewTables(warehouseId: string, since?: string): Promise<WarehouseNewTables> {
  const query = since ? `?since=${encodeURIComponent(since)}` : ''
  return api.get<WarehouseNewTables>(`/api/v1/warehouses/${warehouseId}/new-tables${query}`)
}

export function getWarehouseValidation(warehouseId: string): Promise<WarehouseValidation> {
  return api.get<WarehouseValidation>(`/api/v1/warehouses/${warehouseId}/validation`)
}
