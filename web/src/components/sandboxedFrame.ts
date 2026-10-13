import type { AppColorScheme } from '../utils/appTheme'

/** Child → host frames. Only these shapes are accepted. */
export type FrameMessage =
  | { type: 'ready' }
  | { type: 'request_data'; key: string }
  | { type: 'open_link'; href: string }

/** Host → child frames. */
export type HostMessage =
  | { type: 'init'; theme: Record<string, unknown>; variables: Record<string, unknown>; bindings: Array<{ key: string; status: 'ready' | 'unavailable' }> }
  | { type: 'data'; key: string; result: unknown }
  | { type: 'error'; key?: string; message: string }

export function parseFrameMessage(data: unknown): FrameMessage | null {
  if (!data || typeof data !== 'object') return null
  const m = data as Record<string, unknown>
  if (m.type === 'ready') return { type: 'ready' }
  if (m.type === 'request_data' && typeof m.key === 'string' && m.key.length > 0) return { type: 'request_data', key: m.key }
  if (m.type === 'open_link' && typeof m.href === 'string' && m.href.length > 0) return { type: 'open_link', href: m.href }
  return null
}

export function parseHostMessage(data: unknown): HostMessage | null {
  if (!data || typeof data !== 'object') return null
  const m = data as Record<string, unknown>
  if (m.type === 'init' && typeof m.theme === 'object' && typeof m.variables === 'object' && Array.isArray(m.bindings)) {
    return m as unknown as HostMessage
  }
  if (m.type === 'data' && typeof m.key === 'string') return m as unknown as HostMessage
  if (m.type === 'error' && typeof m.message === 'string') return m as unknown as HostMessage
  return null
}

/** The host's answer to `open_link`: http(s) only, always with noopener. */
export function safeOpenLink(href: string, open: (url: string) => void): boolean {
  let url: URL
  try {
    url = new URL(href, window.location.origin)
  } catch {
    return false
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return false
  open(url.toString())
  return true
}

/** Neutralizes the sequence that can close the injected script element. */
function safeScript(js: string): string {
  return js.replace(/<\/(script)/gi, '<\\/$1')
}

/** Neutralizes the sequence that can close the injected style element. */
function safeStyle(css: string): string {
  return css.replace(/<\/(style)/gi, '<\\/$1')
}

/**
 * Composes the sandbox document: strict CSP with `connect-src 'none'` (the
 * component cannot fetch anything), inline author script/style only, the
 * resolved theme tokens as :root variables, and a small bootstrap that
 * forwards child→host frames and applies host→child data events as
 * `window.__aether` CustomEvents the author script can subscribe to.
 */
export function buildCustomSrcDoc({ html, css, js, tokens, scheme, origin }: {
  html: string
  css: string
  js: string
  tokens: Record<string, string>
  scheme: AppColorScheme
  origin: string
}): string {
  const csp = [
    "default-src 'none'",
    "script-src 'unsafe-inline'",
    "style-src 'unsafe-inline'",
    `img-src data: ${origin}`,
    `font-src data: ${origin}`,
    "connect-src 'none'",
    "form-action 'none'",
    "base-uri 'none'",
  ].join('; ')
  const tokenCss = Object.entries(tokens).map(([k, v]) => `${k}:${v}`).join(';')
  const canvas = scheme === 'dark' ? '#141414' : '#ffffff'
  const text = scheme === 'dark' ? '#e8e8e8' : '#111111'
  const bootstrap = `
(function(){
  var post = function(m){ try { parent.postMessage(m, '*') } catch (e) {} };
  window.addEventListener('message', function(ev){
    if (ev.data && ev.data.type) window.dispatchEvent(new CustomEvent('aether:' + ev.data.type, { detail: ev.data }));
  });
  window.aether = {
    ready: function(){ post({type:'ready'}) },
    requestData: function(key){ post({type:'request_data', key: String(key)}) },
    openLink: function(href){ post({type:'open_link', href: String(href)}) },
  };
  post({type:'ready'});
})();`
  const scrollbars = [
    "*{scrollbar-width:thin;scrollbar-color:#8888 transparent;scrollbar-color:color-mix(in srgb,var(--text-primary,#888) 25%,transparent) transparent}",
    "*::-webkit-scrollbar{width:7px;height:7px}",
    "*::-webkit-scrollbar-track{background:transparent}",
    "*::-webkit-scrollbar-thumb{background:#8888;background:color-mix(in srgb,var(--text-primary,#888) 25%,transparent);border-radius:4px}",
  ].join('')
  return [
    '<!doctype html><html><head><meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${csp}">`,
    `<style>:root{color-scheme:${scheme};${tokenCss}}html,body{margin:0;padding:0;background:var(--bg-card,${canvas});color:var(--text-primary,${text});font-family:var(--app-font-family,'DM Sans',system-ui,sans-serif)}${scrollbars}</style>`,
    `<style>${safeStyle(css)}</style>`,
    `</head><body>${html}<script>${bootstrap}${safeScript(js)}</script></body></html>`,
  ].join('')
}
