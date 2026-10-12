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

  it('computes a mid-range pair', () => {
    // #767676 on white is the canonical 4.5:1 boundary example (4.54:1).
    expect(contrastRatio('#767676', '#ffffff')).toBeCloseTo(4.54, 1)
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

  it('maps font_family to the matching stack', () => {
    const stack = (font: 'dm-sans' | 'jetbrains-mono' | 'system') =>
      resolveAppTheme({ font_family: font }, undefined, 'light').vars['--app-font']
    expect(stack('dm-sans')).toBe("'DM Sans', system-ui, sans-serif")
    expect(stack('jetbrains-mono')).toBe("'JetBrains Mono', ui-monospace, monospace")
    expect(stack('system')).toBe('system-ui, -apple-system, sans-serif')
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

  describe('contrast guardrails', () => {
    it('keeps a text override that clears 4.5:1', () => {
      const resolved = resolveAppTheme({ background: '#111111', text: '#ffffff' }, undefined, 'light')
      expect(resolved.vars['--text-primary']).toBe('#ffffff')
    })

    it('drops a failing text override while keeping the background', () => {
      const resolved = resolveAppTheme({ background: '#111111', text: '#333333' }, undefined, 'light')
      expect(resolved.vars['--bg-primary']).toBe('#111111')
      expect(resolved.vars['--text-primary']).toBeUndefined()
    })

    it('checks a text override against the default canvas when no background is set', () => {
      // #cccccc on the light canvas (#f5f5f5) is ~1.4:1 — dropped.
      expect(resolveAppTheme({ text: '#cccccc' }, undefined, 'light').vars['--text-primary']).toBeUndefined()
      expect(resolveAppTheme({ text: '#111111' }, undefined, 'light').vars['--text-primary']).toBe('#111111')
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
