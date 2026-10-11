import { describe, expect, it, vi } from 'vitest'
import { formatValue } from './valueFormat'

describe('formatValue edge cases', () => {
  it('renders undefined and empty strings as an em dash', () => {
    expect(formatValue(undefined)).toBe('—')
    expect(formatValue('')).toBe('—')
  })
  it('clamps negative decimals to zero instead of throwing', () => {
    expect(formatValue(1.234, { decimals: -1 })).toBe('1')
  })
  it('ignores non-finite decimals instead of throwing', () => {
    expect(formatValue(1.234, { decimals: Number.NaN })).toBe('1.23')
    expect(formatValue(1.234, { decimals: Number.POSITIVE_INFINITY })).toBe('1.23')
  })
  it('falls back to plain number formatting for an invalid currency code', () => {
    // An invalid ISO code makes Intl throw a RangeError; formatting must
    // degrade to plain numbers instead of blanking the render.
    expect(formatValue(1234.5, { style: 'currency', currency: 'EURO' })).toBe('1,234.5')
    expect(formatValue(1234.5, { style: 'currency', currency: 'EURO', decimals: 0 })).toBe('1,235')
  })
  it('reuses cached formatters for identical options', () => {
    const Original = Intl.NumberFormat
    const spy = vi.spyOn(Intl, 'NumberFormat').mockImplementation(
      function (...args: ConstructorParameters<typeof Intl.NumberFormat>) {
        return new Original(...args)
      },
    )
    formatValue(1, { decimals: 7 })
    formatValue(2, { decimals: 7 })
    expect(spy).toHaveBeenCalledTimes(1)
    spy.mockRestore()
  })
})
