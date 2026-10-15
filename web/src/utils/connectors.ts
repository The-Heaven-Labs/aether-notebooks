/** Connector fields the shared execution-picker rule reads. */
export interface PickerConnector {
  id: string
  name: string
  is_provisioner?: boolean
  can_use?: boolean
}

/**
 * True for a provisioner connector this viewer cannot execute through
 * (can_use === false, computed server-side per caller). Only an explicit
 * false blocks — can_use undefined is treated as usable.
 */
export function isBlockedProvisioner(c: Pick<PickerConnector, 'is_provisioner' | 'can_use'>): boolean {
  return c.is_provisioner === true && c.can_use === false
}

/**
 * Option label for a connector in an execution picker: its name plus the
 * reason suffix — "(provisioner — no access)" when execution is blocked,
 * "(provisioner)" for an allowed provisioner, "(view only)" when the viewer
 * may not run through it, and no suffix otherwise.
 */
export function connectorOptionLabel(c: Pick<PickerConnector, 'name' | 'is_provisioner' | 'can_use'>): string {
  const suffix = c.is_provisioner === true
    ? (c.can_use === false ? ' (provisioner — no access)' : ' (provisioner)')
    : (c.can_use === false ? ' (view only)' : '')
  return `${c.name}${suffix}`
}

/**
 * Hides provisioner connectors that cannot execute for the current viewer. A
 * currently-selected hidden connector is kept as a single option so selects
 * never blank out.
 */
export function executableConnectorOptions<T extends PickerConnector>(
  connectors: T[],
  selectedId?: string | null,
): T[] {
  return connectors.filter(c => !isBlockedProvisioner(c) || c.id === selectedId)
}
