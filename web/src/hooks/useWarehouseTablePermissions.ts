import { useQuery } from '@tanstack/react-query'
import { getAuthConfig } from '../api/auth'

// useWarehouseTablePermissions reports whether the server's
// AETHER_CH_TABLE_PERMISSIONS kill switch is enabled. Warehouse copy must not
// claim ClickHouse enforces table grants while the switch is off, so an
// unresolved or failed config reports false: the UI never promises enforcement
// that is not active.
export function useWarehouseTablePermissions(): boolean {
  const { data } = useQuery({
    queryKey: ['auth-config'],
    queryFn: getAuthConfig,
    staleTime: 5 * 60 * 1000,
  })
  return data?.warehouse_table_permissions_enabled ?? false
}
