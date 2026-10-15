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
 * reason suffix — "(provisioner)" for an allowed provisioner, "(view only)"
 * when the viewer may not run through it, and no suffix otherwise.
 *
 * "(provisioner — no access)" is a defensive fallback only: execution pickers
 * never list a blocked provisioner ({@link executableConnectorOptions}), so
 * the branch is unreachable through them.
 */
export function connectorOptionLabel(c: Pick<PickerConnector, 'name' | 'is_provisioner' | 'can_use'>): string {
  const suffix = c.is_provisioner === true
    ? (c.can_use === false ? ' (provisioner — no access)' : ' (provisioner)')
    : (c.can_use === false ? ' (view only)' : '')
  return `${c.name}${suffix}`
}

/**
 * Hides provisioner connectors that cannot execute for the current viewer.
 * A blocked provisioner is never listed — not even when it is the current
 * selection (callers fall back to their placeholder; the stored reference
 * remains until the user picks another connector).
 */
export function executableConnectorOptions<T extends PickerConnector>(
  connectors: T[],
): T[] {
  return connectors.filter(c => !isBlockedProvisioner(c))
}

/**
 * True when a stored connector reference points at something the picker's
 * rendered options do not contain — the placeholder-plus-hint state.
 *
 * This only answers the id question. Callers must still gate on the connector
 * catalog being known (query settled / non-empty prop): an empty or unloaded
 * list hides every value, and "can't tell" must never render as "unavailable".
 */
export function isHiddenSelection(
  value: string | null | undefined,
  options: { id: string }[],
): boolean {
  return value != null && value !== '' && !options.some(o => o.id === value)
}
