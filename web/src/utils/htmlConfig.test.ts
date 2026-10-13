import { describe, expect, it } from 'vitest'
import { htmlConfigFrom } from './htmlConfig'

describe('htmlConfigFrom', () => {
  it('reads panel html and css out of a widget config', () => {
    expect(htmlConfigFrom({ html: '<p>x</p>', css: 'p{}' })).toEqual({ html: '<p>x</p>', css: 'p{}' })
  })
  it('coerces absent or malformed values to empty strings', () => {
    expect(htmlConfigFrom(undefined)).toEqual({ html: '', css: '' })
    expect(htmlConfigFrom({ html: 42, css: { nested: true } })).toEqual({ html: '', css: '' })
    expect(htmlConfigFrom({ html: '<p>x</p>' })).toEqual({ html: '<p>x</p>', css: '' })
  })
})
