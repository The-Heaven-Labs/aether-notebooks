const editCallbacks = new Map<string, () => void>()

export function focusMarkdownCell(cellId: string) {
  editCallbacks.get(cellId)?.()
}

export function setMarkdownFocusCallback(cellId: string, cb: () => void) {
  editCallbacks.set(cellId, cb)
}

export function clearMarkdownFocusCallback(cellId: string) {
  editCallbacks.delete(cellId)
}
