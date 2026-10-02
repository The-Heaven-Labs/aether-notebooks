import { useEffect, useRef } from 'react'

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

  useEffect(() => {
    if (!enabled) return
    const handler = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.preventDefault()
      onCloseRef.current()
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [enabled])
}
