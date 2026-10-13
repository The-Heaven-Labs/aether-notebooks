import { describe, expect, test } from 'vitest'
import { buildCustomSrcDoc, parseHostMessage, parseFrameMessage } from './sandboxedFrame'

const ORIGIN = 'https://app.example.com'

describe('buildCustomSrcDoc', () => {
  test('composes CSP (no network), theme tokens, css, bootstrap, and the author script', () => {
    const doc = buildCustomSrcDoc({
      html: '<div id="root"></div>',
      css: '#root{color:var(--accent)}',
      js: "document.getElementById('root').textContent='hi'",
      tokens: { '--accent': '#7c5cff' },
      scheme: 'dark',
      origin: ORIGIN,
    })
    expect(doc).toContain("default-src 'none'")
    expect(doc).toContain("connect-src 'none'")
    expect(doc).toContain("script-src 'unsafe-inline'")
    expect(doc).toContain(`img-src data: ${ORIGIN}`)
    expect(doc).toContain('--accent:#7c5cff')
    expect(doc).toContain("textContent='hi'")
  })

  test('escapes a </script> breakout in js and keeps css style-safe', () => {
    const doc = buildCustomSrcDoc({
      html: '', css: '</style><script>x</script>',
      js: "const s = '</script><script>alert(1)</script>'",
      tokens: {}, scheme: 'light', origin: ORIGIN,
    })
    expect(doc).not.toContain('</script><script>alert(1)</script>')
  })
})

describe('protocol parsers', () => {
  test('frame messages accept only the known shapes', () => {
    expect(parseFrameMessage({ type: 'ready' })).toEqual({ type: 'ready' })
    expect(parseFrameMessage({ type: 'request_data', key: 'sales' })).toEqual({ type: 'request_data', key: 'sales' })
    expect(parseFrameMessage({ type: 'open_link', href: 'https://x.test' })).toEqual({ type: 'open_link', href: 'https://x.test' })
    expect(parseFrameMessage({ type: 'request_data' })).toBeNull()
    expect(parseFrameMessage({ type: 'nope' })).toBeNull()
    expect(parseFrameMessage('junk')).toBeNull()
  })

  test('host messages tolerate junk payloads from the page', () => {
    expect(parseHostMessage({ type: 'init', theme: {}, variables: {}, bindings: [] })).not.toBeNull()
    expect(parseHostMessage({ type: 'data', key: 'sales', result: { ok: true } })).not.toBeNull()
    expect(parseHostMessage({ type: 'error', message: 'x' })).not.toBeNull()
    expect(parseHostMessage({ type: 'data', key: 7 })).toBeNull()
    expect(parseHostMessage(null)).toBeNull()
  })
})
