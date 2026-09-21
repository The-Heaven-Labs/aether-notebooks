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

export interface WarehouseEffectiveAccess {
  user_id: string
  warehouse_id: string
  ch_user?: string
  ch_roles?: string[]
  tables: WarehouseEffectiveTable[]
  services: WarehouseEffectiveService[]
  preferred_connector_id: string | null
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
  data: { name?: string; provisioner_connector_id?: string | null },
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
