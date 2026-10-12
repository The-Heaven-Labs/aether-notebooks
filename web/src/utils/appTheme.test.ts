import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { contrastRatio, hexToRgb, resolveAppTheme } from './appTheme'

describe('hexToRgb', () => {
  it('parses #rrggbb', () => {
    expect(hexToRgb('#7c6faa')).toEqual([124, 111, 170])
  })

  it('expands #rgb shorthand', () => {
    expect(hexToRgb('#abc')).toEqual([0xaa, 0xbb, 0xcc])
  })

  it('is case-insensitive', () => {
    expect(hexToRgb('#ABC')).toEqual([0xaa, 0xbb, 0xcc])
  })

  it('returns null for anything that is not a hex color', () => {
    expect(hexToRgb('purple')).toBeNull()
    expect(hexToRgb('#12345')).toBeNull()
    expect(hexToRgb('rgb(0, 0, 0)')).toBeNull()
    expect(hexToRgb('')).toBeNull()
  })
})

describe('contrastRatio', () => {
  it('scores black on white at the WCAG maximum of 21:1', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 3)
  })

  it('is order-independent', () => {
    expect(contrastRatio('#ffffff', '#000000')).toBeCloseTo(21, 3)
  })

  it('expands shorthand hex', () => {
    expect(contrastRatio('#000', '#fff')).toBeCloseTo(21, 3)
  })

  it('computes the 4.5:1 boundary pair precisely', () => {
    // #767676 on white is just above the threshold; #777777 is just below.
    expect(contrastRatio('#767676', '#ffffff')).toBeCloseTo(4.542, 3)
    expect(contrastRatio('#777777', '#ffffff')).toBeCloseTo(4.478, 3)
  })

  it('returns null when a color cannot be parsed', () => {
    expect(contrastRatio('not-a-color', '#ffffff')).toBeNull()
    expect(contrastRatio('#ffffff', 'rgb(0, 0, 0)')).toBeNull()
  })
})

describe('resolveAppTheme', () => {
  it('returns no overrides when the dashboard and org have no theme', () => {
    const resolved = resolveAppTheme(undefined, undefined, 'light')
    expect(resolved).toEqual({ scheme: 'light', vars: {} })
    expect(resolved.chartPalette).toBeUndefined()
  })

  it('lets dashboard tokens win over branding', () => {
    const resolved = resolveAppTheme({ accent: '#7c6faa' }, { accent_color: '#123456' }, 'light')
    expect(resolved.vars['--accent']).toBe('#7c6faa')
  })

  it('falls back to the branding accent when the dashboard has none', () => {
    const resolved = resolveAppTheme({}, { accent_color: '#123456' }, 'light')
    expect(resolved.vars['--accent']).toBe('#123456')
  })

  it('maps color tokens to the app-surface variables', () => {
    const resolved = resolveAppTheme(
      {
        accent: '#7c6faa',
        background: '#ffffff',
        surface: '#fafafa',
        text: '#111111',
        border: '#e0e0e0',
      },
      undefined,
      'light',
    )
    expect(resolved.vars).toEqual({
      '--accent': '#7c6faa',
      '--bg-primary': '#ffffff',
      '--bg-card': '#fafafa',
      '--text-primary': '#111111',
      '--border-light': '#e0e0e0',
    })
  })

  it('maps radius to --app-radius in pixels', () => {
    expect(resolveAppTheme({ radius: 12 }, undefined, 'light').vars['--app-radius']).toBe('12px')
  })

  it('accepts a zero radius and drops a negative one', () => {
    expect(resolveAppTheme({ radius: 0 }, undefined, 'light').vars['--app-radius']).toBe('0px')
    expect(resolveAppTheme({ radius: -1 }, undefined, 'light').vars['--app-radius']).toBeUndefined()
  })

  it('maps font_family to --app-font and the scoped --font-sans stack', () => {
    const vars = (font: 'dm-sans' | 'jetbrains-mono' | 'system') =>
      resolveAppTheme({ font_family: font }, undefined, 'light').vars
    expect(vars('dm-sans')['--app-font']).toBe("'DM Sans', system-ui, sans-serif")
    expect(vars('jetbrains-mono')['--app-font']).toBe("'JetBrains Mono', ui-monospace, monospace")
    expect(vars('system')['--app-font']).toBe('system-ui, -apple-system, sans-serif')
    // App-surface components set `font-family: var(--font-sans)` explicitly, so
    // the chosen stack must also override the scoped sans token; the data /
    // instrument font (--font-mono) is deliberately left alone.
    expect(vars('dm-sans')['--font-sans']).toBe("'DM Sans', system-ui, sans-serif")
    expect(vars('jetbrains-mono')['--font-sans']).toBe("'JetBrains Mono', ui-monospace, monospace")
    expect(vars('system')['--font-sans']).toBe('system-ui, -apple-system, sans-serif')
    expect(vars('dm-sans')['--font-mono']).toBeUndefined()
  })

  it('ignores overrides that are not hex colors', () => {
    const resolved = resolveAppTheme(
      {
        accent: 'red',
        background: 'blue',
        surface: 'rgb(0, 0, 0)',
        text: '#12345',
        border: 'var(--border)',
      },
      undefined,
      'light',
    )
    expect(resolved.vars).toEqual({})
  })

  it('trims whitespace from color overrides and palette entries', () => {
    const resolved = resolveAppTheme(
      { accent: ' #7c6faa ', chart_palette: [' #123456 ', ' #abc '] },
      undefined,
      'light',
    )
    expect(resolved.vars['--accent']).toBe('#7c6faa')
    expect(resolved.chartPalette).toEqual(['#123456', '#abc'])
  })

  describe('contrast guardrails', () => {
    it('keeps a text override that clears 4.5:1 on the canvas and the cards', () => {
      const resolved = resolveAppTheme(
        { background: '#111111', surface: '#1a1a1a', text: '#ffffff' },
        undefined,
        'light',
      )
      expect(resolved.vars['--bg-primary']).toBe('#111111')
      expect(resolved.vars['--bg-card']).toBe('#1a1a1a')
      expect(resolved.vars['--text-primary']).toBe('#ffffff')
    })

    it('drops a failing text override while keeping the background', () => {
      // #dddddd on the #f0f0f0 canvas is ~1.2:1 — dropped; the default dark
      // text is readable there, so the background stays.
      const resolved = resolveAppTheme({ background: '#f0f0f0', text: '#dddddd' }, undefined, 'light')
      expect(resolved.vars['--bg-primary']).toBe('#f0f0f0')
      expect(resolved.vars['--text-primary']).toBeUndefined()
    })

    it('checks a text override against the default canvas and card when neither is set', () => {
      // Light: #cccccc is unreadable on both defaults; #111111 clears both.
      expect(resolveAppTheme({ text: '#cccccc' }, undefined, 'light').vars['--text-primary']).toBeUndefined()
      expect(resolveAppTheme({ text: '#111111' }, undefined, 'light').vars['--text-primary']).toBe('#111111')
      // Dark: #333333 is unreadable on both defaults; #e8e8e8 clears both.
      expect(resolveAppTheme({ text: '#333333' }, undefined, 'dark').vars['--text-primary']).toBeUndefined()
      expect(resolveAppTheme({ text: '#e8e8e8' }, undefined, 'dark').vars['--text-primary']).toBe('#e8e8e8')
    })

    it('enforces the 4.5:1 boundary on a text override', () => {
      const kept = resolveAppTheme({ background: '#ffffff', text: '#767676' }, undefined, 'light')
      expect(kept.vars['--text-primary']).toBe('#767676')
      const dropped = resolveAppTheme({ background: '#ffffff', text: '#777777' }, undefined, 'light')
      expect(dropped.vars['--text-primary']).toBeUndefined()
      expect(dropped.vars['--bg-primary']).toBe('#ffffff')
    })

    it('drops a light-scheme background the default text cannot be read on', () => {
      // Default #111 text on a #111111 canvas is 1:1 — the canvas reverts.
      expect(resolveAppTheme({ background: '#111111' }, undefined, 'light').vars).toEqual({})
    })

    it('drops a dark-scheme background the default text cannot be read on', () => {
      // Default #e8e8e8 text on white is ~1.2:1 — the canvas reverts.
      expect(resolveAppTheme({ background: '#ffffff' }, undefined, 'dark').vars).toEqual({})
    })

    it('drops a text override that is unreadable on an overridden card', () => {
      // The text clears the white canvas but vanishes on the #111111 cards.
      // Once the override drops, the default text is unreadable there too, so
      // the card surface reverts as well.
      const resolved = resolveAppTheme(
        { background: '#ffffff', surface: '#111111', text: '#111111' },
        undefined,
        'light',
      )
      expect(resolved.vars).toEqual({ '--bg-primary': '#ffffff' })
    })

    it('re-validates the accent against the reverted canvas', () => {
      // The #111111 canvas reverts (above); #9b8fc4 then misses 3:1 on the
      // default light canvas even though it cleared the dark one.
      expect(resolveAppTheme({ background: '#111111', accent: '#9b8fc4' }, undefined, 'light').vars).toEqual({})
    })

    it('keeps a fully readable override set', () => {
      const resolved = resolveAppTheme(
        {
          accent: '#9b8fc4',
          background: '#111111',
          surface: '#1a1a1a',
          text: '#ffffff',
          border: '#2e2e2e',
          radius: 10,
          font_family: 'dm-sans',
        },
        undefined,
        'light',
      )
      expect(resolved.vars).toEqual({
        '--accent': '#9b8fc4',
        '--bg-primary': '#111111',
        '--bg-card': '#1a1a1a',
        '--text-primary': '#ffffff',
        '--border-light': '#2e2e2e',
        '--app-radius': '10px',
        '--app-font': "'DM Sans', system-ui, sans-serif",
        '--font-sans': "'DM Sans', system-ui, sans-serif",
      })
    })

    it('keeps an accent override that clears 3:1', () => {
      const resolved = resolveAppTheme({ background: '#ffffff', accent: '#7c6faa' }, undefined, 'light')
      expect(resolved.vars['--accent']).toBe('#7c6faa')
    })

    it('drops a failing accent override', () => {
      const resolved = resolveAppTheme({ background: '#ffffff', accent: '#f0f0f0' }, undefined, 'light')
      expect(resolved.vars['--bg-primary']).toBe('#ffffff')
      expect(resolved.vars['--accent']).toBeUndefined()
    })

    it('falls back to the branding accent when the dashboard accent fails contrast', () => {
      const resolved = resolveAppTheme(
        { background: '#ffffff', accent: '#f0f0f0' },
        { accent_color: '#123456' },
        'light',
      )
      expect(resolved.vars['--accent']).toBe('#123456')
    })
  })

  describe('color scheme', () => {
    it('auto follows the global scheme', () => {
      expect(resolveAppTheme(undefined, undefined, 'dark').scheme).toBe('dark')
      expect(resolveAppTheme({ color_scheme: 'auto' }, undefined, 'light').scheme).toBe('light')
    })

    it('light and dark force the scheme regardless of the global theme', () => {
      expect(resolveAppTheme({ color_scheme: 'light' }, undefined, 'dark').scheme).toBe('light')
      expect(resolveAppTheme({ color_scheme: 'dark' }, undefined, 'light').scheme).toBe('dark')
    })
  })

  describe('chart palette', () => {
    it('lets the dashboard palette win over branding', () => {
      const resolved = resolveAppTheme(
        { chart_palette: ['#111111'] },
        { default_chart_palette: ['#222222'] },
        'light',
      )
      expect(resolved.chartPalette).toEqual(['#111111'])
    })

    it('uses the branding palette when the dashboard has none', () => {
      const resolved = resolveAppTheme({}, { default_chart_palette: ['#222222', '#333333'] }, 'light')
      expect(resolved.chartPalette).toEqual(['#222222', '#333333'])
    })

    it('is undefined when neither source provides a palette', () => {
      expect(resolveAppTheme({}, {}, 'light').chartPalette).toBeUndefined()
    })

    it('filters invalid entries', () => {
      const resolved = resolveAppTheme(
        { chart_palette: ['#123456', 'not-a-color', '#abc', '#12345'] },
        undefined,
        'light',
      )
      expect(resolved.chartPalette).toEqual(['#123456', '#abc'])
    })

    it('falls back to branding when the dashboard palette has no valid entries', () => {
      const resolved = resolveAppTheme(
        { chart_palette: ['not-a-color'] },
        { default_chart_palette: ['#222222'] },
        'light',
      )
      expect(resolved.chartPalette).toEqual(['#222222'])
    })
  })
})

describe('theme.css app-scheme blocks', () => {
  // Vitest runs with cwd = the web/ project root (Taskfile `dir: web`,
  // CI `cd web && …`); `import.meta.url` is not a file: URL under the jsdom
  // transform, so the path is resolved against the runner's cwd.
  const css = readFileSync('src/styles/theme.css', 'utf8')

  const declarations = (selector: string): Record<string, string> => {
    const pattern = new RegExp(`${selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*\\{([^}]*)\\}`)
    const match = pattern.exec(css)
    if (!match) throw new Error(`theme.css block not found: ${selector}`)
    const decls: Record<string, string> = {}
    for (const line of match[1].split('\n')) {
      const trimmed = line.trim()
      if (!trimmed || trimmed.startsWith('/*') || trimmed.startsWith('*')) continue
      const colon = trimmed.indexOf(':')
      if (colon === -1) continue
      decls[trimmed.slice(0, colon).trim()] = trimmed.slice(colon + 1).trim().replace(/;$/, '')
    }
    return decls
  }

  it('re-declares every scheme-dependent token in both app-scheme blocks', () => {
    const root = declarations(':root')
    const dark = declarations('[data-theme="dark"]')
    const light = declarations(".app-theme[data-app-scheme='light']")
    const appDark = declarations(".app-theme[data-app-scheme='dark']")

    // Targeted drift check: only tokens whose values actually differ between
    // the global blocks (scheme-independent tokens keep their :root value in
    // both schemes), plus the var() indirection pair, which resolves per
    // scheme even though its declared text is identical.
    const schemeDependent = new Set<string>(['--accent-bg', '--accent-border'])
    for (const token of Object.keys(root)) {
      if (token in dark && root[token] !== dark[token]) schemeDependent.add(token)
    }
    for (const token of Object.keys(dark)) {
      if (token in root && root[token] !== dark[token]) schemeDependent.add(token)
    }

    const missing = {
      light: [...schemeDependent].filter(token => !(token in light)).sort(),
      dark: [...schemeDependent].filter(token => !(token in appDark)).sort(),
    }
    expect(missing).toEqual({ light: [], dark: [] })
  })
})
