export interface GroupLike {
  name: string
  display_name?: string | null
}

/** Renders the admin-owned label, falling back to the sync identity name. */
export function groupLabel(group: GroupLike): string {
  const label = group.display_name?.trim()
  return label ? label : group.name
}
