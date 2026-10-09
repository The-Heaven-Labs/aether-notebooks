import { useEffect, type RefObject } from 'react'

const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(', ')

function focusableWithin(el: HTMLElement): HTMLElement[] {
  return Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE))
    // getClientRects (not offsetParent) also keeps fixed-position overlays.
    .filter(n => n.getClientRects().length > 0)
}

/**
 * Moves focus into a dialog on mount, keeps Tab cycling inside it, and
 * restores focus to the previously active element on unmount. The container
 * should carry tabIndex={-1}. Pass initialFocus to start on a named control.
 */
export function useFocusTrap(
  ref: RefObject<HTMLElement | null>,
  active = true,
  initialFocus?: RefObject<HTMLElement | null>,
) {
  useEffect(() => {
    if (!active) return
    const el = ref.current
    if (!el) return
    const previouslyFocused = document.activeElement as HTMLElement | null
    ;(initialFocus?.current ?? el).focus()

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return
      const nodes = focusableWithin(el)
      if (!nodes.length) {
        e.preventDefault()
        el.focus()
        return
      }
      const first = nodes[0]
      const last = nodes[nodes.length - 1]
      const current = document.activeElement
      if (!el.contains(current)) {
        e.preventDefault()
        ;(e.shiftKey ? last : first).focus()
        return
      }
      if (e.shiftKey && current === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && current === last) {
        e.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', onKeyDown, true)
    return () => {
      document.removeEventListener('keydown', onKeyDown, true)
      // Only restore when the dialog didn't hand focus to something newer.
      if (document.body.contains(previouslyFocused)) previouslyFocused?.focus?.()
    }
  }, [ref, active])
}
