import { describe, expect, it, test } from 'vitest'
import { interpolateHtml, interpolateText } from './interpolateText'
import type { DashboardVariable } from '../types'

const vars: DashboardVariable[] = [
  { name: 'region', label: 'Region', type: 'text' },
  { name: 'regions', label: 'Regions', type: 'multi_select' },
  { name: 'window', label: 'Window', type: 'date_range' },
  { name: 'count', label: 'Count', type: 'number' },
  { name: 'active', label: 'Active', type: 'boolean' },
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
  it('joins two date-like values for a multi_select instead of formatting a range', () => {
    expect(interpolateText('{{regions}}', { regions: ['2026-10-01', '2026-10-05'] }, vars)).toBe('2026-10-01, 2026-10-05')
  })
  it('neutralizes markdown link and image syntax in values', () => {
    expect(interpolateText('{{region}}', { region: '[click](https://evil.example)' }, vars)).toBe('\\[click\\](https://evil.example)')
    expect(interpolateText('{{region}}', { region: '![x](https://evil.example)' }, vars)).toBe('\\!\\[x\\](https://evil.example)')
  })
  it('escapes backslashes before escaping brackets and exclamation marks', () => {
    expect(interpolateText('{{region}}', { region: 'a\\b' }, vars)).toBe('a\\\\b')
    expect(interpolateText('{{region}}', { region: '\\[x]' }, vars)).toBe('\\\\\\[x\\]')
  })
  it('renders number and boolean values as text', () => {
    expect(interpolateText('{{count}}', { count: 42 }, vars)).toBe('42')
    expect(interpolateText('{{active}}', { active: true }, vars)).toBe('true')
  })
  it('substitutes whitespace-padded tokens', () => {
    expect(interpolateText('{{ region }}', { region: 'EMEA' }, vars)).toBe('EMEA')
  })
})

describe('interpolateHtml', () => {
  const variables: DashboardVariable[] = [
    { name: 'region', label: 'Region', type: 'text' },
    { name: 'window', label: 'Window', type: 'date_range' },
  ]

  test('escapes values for both text and attribute contexts', () => {
    const out = interpolateHtml('<div title="{{region}}">{{region}}</div>', { region: '<b>"EU" & \'west\'</b>' }, variables)
    expect(out).toBe('<div title="&lt;b&gt;&quot;EU&quot; &amp; &#39;west&#39;&lt;/b&gt;">&lt;b&gt;&quot;EU&quot; &amp; &#39;west&#39;&lt;/b&gt;</div>')
  })

  test('unknown tokens stay visible; date_range renders start → end', () => {
    expect(interpolateHtml('{{nope}}', {}, variables)).toBe('{{nope}}')
    expect(interpolateHtml('{{window}}', { window: ['2026-01-01', '2026-02-01'] }, variables)).toBe('2026-01-01 → 2026-02-01')
  })
})
