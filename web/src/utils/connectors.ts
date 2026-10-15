/**
 * Hides provisioner connectors that cannot execute for anyone (can_use === false,
 * computed server-side). A currently-selected hidden connector is kept as a
 * single disabled option so selects never blank out.
 */
export function executableConnectorOptions<
  T extends { id: string; is_provisioner?: boolean; can_use?: boolean },
>(connectors: T[], selectedId?: string | null): T[] {
  return connectors.filter(
    (c) => !(c.is_provisioner === true && c.can_use === false) || c.id === selectedId,
  )
}
