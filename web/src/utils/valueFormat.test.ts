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

describe('formatValue compact', () => {
  it('compacts currency values instead of ignoring the flag', () => {
    // The metric/table drawers pair currency with compact; Intl natively
    // supports the combination ($1.3M), so compact must apply after style.
    expect(formatValue(1250000, { style: 'currency', compact: true })).toBe('$1.3M')
  })
  it('honors decimals when compacting currency', () => {
    expect(formatValue(1250000, { style: 'currency', compact: true, decimals: 2 })).toBe('$1.25M')
    expect(formatValue(1250000, { style: 'currency', compact: true, decimals: 0 })).toBe('$1M')
  })
  it('leaves non-compact currency unchanged', () => {
    expect(formatValue(1250000, { style: 'currency' })).toBe('$1,250,000.00')
    expect(formatValue(1250000, { style: 'currency', decimals: 0 })).toBe('$1,250,000')
  })
  it('keeps plain-number compact behavior unchanged', () => {
    expect(formatValue(1250000, { compact: true })).toBe('1.3M')
    expect(formatValue(1250000, { compact: true, decimals: 0 })).toBe('1M')
  })
})
