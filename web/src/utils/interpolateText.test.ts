import { describe, expect, it } from 'vitest'
import { interpolateText } from './interpolateText'
import type { DashboardVariable } from '../types'

const vars: DashboardVariable[] = [
  { name: 'region', label: 'Region', type: 'text' },
  { name: 'regions', label: 'Regions', type: 'multi_select' },
  { name: 'window', label: 'Window', type: 'date_range' },
]

describe('interpolateText', () => {
  it('substitutes declared variables', () => {
    expect(interpolateText('## {{region}} overview', { region: 'EMEA' }, vars)).toBe('## EMEA overview')
  })
  it('joins multi-select values and formats date ranges', () => {
    expect(interpolateText('{{regions}}', { regions: ['EMEA', 'AMER'] }, vars)).toBe('EMEA, AMER')
    expect(interpolateText('{{window}}', { window: ['2026-10-01', '2026-10-07'] }, vars)).toBe('2026-10-01 → 2026-10-07')
  })
  it('escapes HTML-significant characters in values', () => {
    expect(interpolateText('{{region}}', { region: '<b>&</b>' }, vars)).toBe('&lt;b&gt;&amp;&lt;/b&gt;')
  })
  it('leaves unknown tokens untouched', () => {
    expect(interpolateText('{{mystery}}', { mystery: 'x' }, vars)).toBe('{{mystery}}')
  })
  it('renders missing values as empty', () => {
    expect(interpolateText('a{{region}}b', {}, vars)).toBe('ab')
  })
})
