import { afterEach, describe, expect, test, vi } from 'vitest'
import { isApplePlatform } from './platform'

describe('isApplePlatform', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  test('true on macOS and iOS browsers', () => {
    vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)' })
    expect(isApplePlatform()).toBe(true)

    vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)' })
    expect(isApplePlatform()).toBe(true)
  })

  test('false on Windows and Linux browsers', () => {
    vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' })
    expect(isApplePlatform()).toBe(false)

    vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (X11; Linux x86_64)' })
    expect(isApplePlatform()).toBe(false)
  })

  test('false when no navigator global exists', () => {
    vi.stubGlobal('navigator', undefined)
    expect(typeof navigator).toBe('undefined')
    expect(isApplePlatform()).toBe(false)
  })
})
