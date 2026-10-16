import { describe, test, expect } from 'vitest'
import { APP_CONTENT_MAX_WIDTH, appContentWidthStyle } from './appLayout'

describe('APP_CONTENT_MAX_WIDTH', () => {
  test('is the shared 1200px app content width', () => {
    expect(APP_CONTENT_MAX_WIDTH).toBe(1200)
  })
})

describe('appContentWidthStyle', () => {
  test('constrains a fixed width to the shared max width, centered', () => {
    expect(appContentWidthStyle('fixed')).toEqual({
      maxWidth: APP_CONTENT_MAX_WIDTH,
      margin: '0 auto',
    })
  })

  test('treats an absent width as fixed (the default)', () => {
    expect(appContentWidthStyle(undefined)).toEqual({
      maxWidth: APP_CONTENT_MAX_WIDTH,
      margin: '0 auto',
    })
  })

  test('returns undefined for a full width so callers stay unconstrained', () => {
    expect(appContentWidthStyle('full')).toBeUndefined()
  })
})
