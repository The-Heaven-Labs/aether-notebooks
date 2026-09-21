import { api } from './client'
import type { ConnectorSchema } from '../types'

export function connectorSchemaQueryKey(connectorId: string) {
  return ['connector-schema', connectorId] as const
}

export function getConnectorSchema(connectorId: string): Promise<ConnectorSchema> {
  return api.get<ConnectorSchema>(`/api/v1/connectors/${connectorId}/schema`)
}
