import { describe, expect, it } from 'vitest'
import { appearanceFrom } from './widgetAppearance'

describe('appearanceFrom', () => {
  it('defaults to today’s card look', () => {
    expect(appearanceFrom(undefined)).toEqual({ background: 'card', border: true, padding: 'none', align: 'left' })
  })
  it('reads explicit values', () => {
    expect(appearanceFrom({ appearance: { background: 'transparent', border: false, padding: 'none', align: 'center' } }))
      .toEqual({ background: 'transparent', border: false, padding: 'none', align: 'center' })
  })
  it('ignores invalid values', () => {
    expect(appearanceFrom({ appearance: { background: 'neon', padding: 'xl' } }).background).toBe('card')
  })

  it('reads every valid padding and align value', () => {
    expect(appearanceFrom({ appearance: { padding: 'sm' } }).padding).toBe('sm')
    expect(appearanceFrom({ appearance: { padding: 'md' } }).padding).toBe('md')
    expect(appearanceFrom({ appearance: { align: 'right' } }).align).toBe('right')
  })

  it('falls back per-field when values are invalid', () => {
    expect(appearanceFrom({ appearance: { background: 'neon', border: 'yes', padding: 'xl', align: 'justify' } }))
      .toEqual({ background: 'card', border: true, padding: 'none', align: 'left' })
  })

  it('ignores a non-object appearance block', () => {
    const defaults = { background: 'card', border: true, padding: 'none', align: 'left' }
    expect(appearanceFrom({ appearance: 'transparent' })).toEqual(defaults)
    expect(appearanceFrom({ appearance: null })).toEqual(defaults)
    expect(appearanceFrom({ appearance: ['card'] })).toEqual(defaults)
  })
})
