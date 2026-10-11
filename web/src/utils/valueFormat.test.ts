import { describe, expect, it } from 'vitest'
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
})
