import { describe, expect, it } from 'vitest'
import { customConfigFrom } from './customConfig'

describe('customConfigFrom', () => {
  it('reads the bundle and drops malformed bindings', () => {
    expect(customConfigFrom({ html: '<p>x</p>', css: 'p{}', js: '1', bindings: [
      { key: 'sales', widget_id: 'w1' }, { key: 'bad' }, 'junk',
    ] })).toEqual({ html: '<p>x</p>', css: 'p{}', js: '1', bindings: [{ key: 'sales', widget_id: 'w1' }] })
  })
  it('coerces absent values', () => {
    expect(customConfigFrom(undefined)).toEqual({ html: '', css: '', js: '', bindings: [] })
  })
})
