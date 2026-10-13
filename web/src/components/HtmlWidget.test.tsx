import { describe, expect, test } from 'vitest'
import { render, screen } from '@testing-library/react'
import { HtmlWidget, sanitizePanelHtml, buildPanelSrcDoc } from './HtmlWidget'
import type { DashboardVariable } from '../types'

const ORIGIN = 'https://app.example.com'

describe('sanitizePanelHtml', () => {
  test('strips scripts, event handlers, links, and forms', () => {
    const out = sanitizePanelHtml('<script>alert(1)</script><a href="/x">link</a><form><input></form><div onclick="x">ok</div>', ORIGIN)
    expect(out).not.toContain('<script')
    expect(out).not.toContain('<a ')
    expect(out).not.toContain('<form')
    expect(out).not.toContain('onclick')
    expect(out).toContain('<div>ok</div>')
  })

  test('restricts image sources to data: and the app origin', () => {
    const out = sanitizePanelHtml(
      '<img src="https://evil.example/x.png"><img src="//evil.example/x.png"><img src="data:image/png;base64,AAA"><img src="/api/v1/attachments/1"><img src="https://app.example.com/logo.png">',
      ORIGIN,
    )
    expect(out).not.toContain('evil.example')
    expect(out).toContain('data:image/png')
    expect(out).toContain('/api/v1/attachments/1')
    expect(out).toContain('https://app.example.com/logo.png')
  })
})

describe('buildPanelSrcDoc', () => {
  test('composes CSP, theme tokens, css, and body', () => {
    const doc = buildPanelSrcDoc({
      html: '<div class="stat">42</div>',
      css: '.stat{color:var(--accent)}',
      tokens: { '--accent': '#7c5cff' },
      scheme: 'dark',
      origin: ORIGIN,
    })
    expect(doc).toContain(`img-src data: ${ORIGIN}`)
    expect(doc).toContain("default-src 'none'")
    expect(doc).toContain('--accent:#7c5cff')
    expect(doc).toContain('.stat{color:var(--accent)}')
  })

  test('neutralizes a </style> breakout in css', () => {
    const doc = buildPanelSrcDoc({ html: '', css: '</style><script>alert(1)</script>', tokens: {}, scheme: 'light', origin: ORIGIN })
    expect(doc).not.toContain('</style><script')
  })
})

const variables: DashboardVariable[] = [{ name: 'region', label: 'Region', type: 'text' }]

describe('HtmlWidget', () => {
  test('renders a fully locked-down iframe with interpolated content', () => {
    render(<HtmlWidget html={'<p>{{region}}</p>'} css="" values={{ region: '<b>x</b>' }} variables={variables} />)
    const frame = screen.getByTitle('HTML panel')
    expect(frame.tagName).toBe('IFRAME')
    expect(frame).toHaveAttribute('sandbox', '')
    expect(frame).toHaveAttribute('referrerpolicy', 'no-referrer')
    const doc = frame.getAttribute('srcdoc') ?? ''
    expect(doc).toContain('&lt;b&gt;x&lt;/b&gt;')
    expect(doc).not.toContain('<b>x</b>')
  })

  test('disabled renders the org placeholder; empty renders the empty state', () => {
    const { rerender } = render(<HtmlWidget html="<p>hi</p>" css="" enabled={false} />)
    expect(screen.getByText('HTML panels are disabled for this organization.')).toBeInTheDocument()
    expect(screen.queryByTitle('HTML panel')).toBeNull()

    rerender(<HtmlWidget html="" css="" />)
    expect(screen.getByText(/Empty HTML panel/)).toBeInTheDocument()
    expect(screen.queryByTitle('HTML panel')).toBeNull()
  })
})

describe('HtmlWidget theme fallback (editor preview)', () => {
  test('resolves the explicit dashboard theme when no AppThemeProvider wraps it', () => {
    render(
      <HtmlWidget html="<p>x</p>" css="" theme={{ accent: '#7c5cff', color_scheme: 'dark' }} />,
    )
    const doc = screen.getByTitle('HTML panel').getAttribute('srcdoc') ?? ''
    expect(doc).toContain('--accent:#7c5cff')
    expect(doc).toContain('#141414')
  })
})
