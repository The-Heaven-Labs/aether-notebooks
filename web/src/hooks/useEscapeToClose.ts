import { useEffect, useRef } from 'react'

// Module-level stack of active Escape owners. Only the topmost owner reacts,
// so stacked overlays (drawer + chart config, modal + confirm) never close
// two layers on a single Escape. Later mounts land on top, matching the
// order in which the user opened them.
const escapeStack: symbol[] = []

/**
 * Calls onClose when the user presses Escape, while enabled.
 *
 * The latest handler is kept in a ref so re-renders don't re-subscribe (and
 * don't miss a press between render and effect). Overlay-style panels that
 * sit above another closable panel pass enabled=false to yield Escape to the
 * panel on top.
 */
export function useEscapeToClose(onClose: () => void, enabled = true) {
  const onCloseRef = useRef(onClose)
  useEffect(() => {
    onCloseRef.current = onClose
  }, [onClose])

  const idRef = useRef<symbol | null>(null)
  if (!idRef.current) idRef.current = Symbol('escape-owner')

  useEffect(() => {
    if (!enabled) return
    const id = idRef.current!
    escapeStack.push(id)
    const handler = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      if (escapeStack[escapeStack.length - 1] !== id) return
      e.preventDefault()
      onCloseRef.current()
    }
    document.addEventListener('keydown', handler)
    return () => {
      document.removeEventListener('keydown', handler)
      const index = escapeStack.lastIndexOf(id)
      if (index >= 0) escapeStack.splice(index, 1)
    }
  }, [enabled])
}
