import { describe, expect, it } from 'vitest'
import { groupLabel } from './groupLabel'

describe('groupLabel', () => {
  it('renders the display name when set', () => {
    expect(groupLabel({ name: 'aether-analysts', display_name: 'Data Analysts Infra' })).toBe(
      'Data Analysts Infra',
    )
  })

  it('falls back to the name when the label is blank', () => {
    expect(groupLabel({ name: 'aether-analysts', display_name: '   ' })).toBe('aether-analysts')
  })

  it('falls back to the name when the label is null', () => {
    expect(groupLabel({ name: 'aether-analysts', display_name: null })).toBe('aether-analysts')
  })

  it('falls back to the name when the label is absent', () => {
    expect(groupLabel({ name: 'aether-analysts' })).toBe('aether-analysts')
  })
})
