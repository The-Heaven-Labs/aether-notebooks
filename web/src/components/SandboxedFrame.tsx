import { useEffect, useMemo, useRef } from 'react'
import { buildCustomSrcDoc, parseFrameMessage, safeOpenLink } from './sandboxedFrame'
import type { FrameMessage, HostMessage } from './sandboxedFrame'
import { useResolvedAppTheme } from './AppThemeProvider'
import type { DashboardAppTheme, OrgBranding } from '../types'

interface SandboxedFrameProps {
  html: string
  css: string
  js: string
  /** Called with every accepted child frame (host decides the answer). */
  onMessage: (message: FrameMessage, reply: (message: HostMessage) => void) => void
  theme?: DashboardAppTheme | null
  branding?: OrgBranding | null
  title?: string
}

/**
 * The opaque-origin sandbox for custom components: `sandbox="allow-scripts"`
 * with no `allow-same-origin` (no cookies, storage, or authenticated fetch),
 * a CSP that blocks every outbound request, and a postMessage bridge whose
 * frames are validated against this iframe's contentWindow before dispatch.
 * The child's bootstrap posts `ready`; the host answers it with `init` through
 * the same `onMessage` callback (one flow, no load-event ordering races).
 */
export function SandboxedFrame({ html, css, js, onMessage, theme, branding, title = 'Custom component' }: SandboxedFrameProps) {
  const ref = useRef<HTMLIFrameElement | null>(null)
  const resolved = useResolvedAppTheme(theme, branding)
  const srcDoc = useMemo(
    () => buildCustomSrcDoc({ html, css, js, tokens: resolved.vars, scheme: resolved.scheme, origin: window.location.origin }),
    [html, css, js, resolved],
  )

  useEffect(() => {
    const handler = (event: MessageEvent) => {
      const frame = ref.current
      if (!frame || event.source !== frame.contentWindow) return
      // Opaque origins serialize to "null"; anything else is not our frame.
      if (event.origin !== 'null' && event.origin !== '') return
      const message = parseFrameMessage(event.data)
      if (!message) return
      const reply = (out: HostMessage) => {
        try { frame.contentWindow?.postMessage(out, '*') } catch { /* frame gone */ }
      }
      if (message.type === 'open_link') {
        safeOpenLink(message.href, (url) => window.open(url, '_blank', 'noopener,noreferrer'))
        return
      }
      onMessage(message, reply)
    }
    window.addEventListener('message', handler)
    return () => window.removeEventListener('message', handler)
  }, [onMessage])

  return (
    <iframe
      ref={ref}
      title={title}
      sandbox="allow-scripts"
      referrerPolicy="no-referrer"
      srcDoc={srcDoc}
      style={{ width: '100%', height: '100%', border: 'none', display: 'block' }}
    />
  )
}
