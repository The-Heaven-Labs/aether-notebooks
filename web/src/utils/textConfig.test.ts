import { describe, expect, it } from 'vitest'
import { textConfigFrom } from './textConfig'

describe('textConfigFrom', () => {
  it('defaults to empty when the config is absent', () => {
    expect(textConfigFrom(undefined)).toEqual({ markdown: '' })
  })
  it('defaults to empty when config.text is absent', () => {
    expect(textConfigFrom({})).toEqual({ markdown: '' })
  })
  it('defaults to empty when config.text is not an object', () => {
    expect(textConfigFrom({ text: 'x' })).toEqual({ markdown: '' })
  })
  it('defaults to empty when markdown is not a string', () => {
    expect(textConfigFrom({ text: { markdown: 42 } })).toEqual({ markdown: '' })
  })
  it('reads config.text.markdown', () => {
    expect(textConfigFrom({ text: { markdown: '## hi' } })).toEqual({ markdown: '## hi' })
  })
})
