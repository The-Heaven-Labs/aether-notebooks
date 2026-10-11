import { describe, expect, it } from 'vitest'
import { tableConfigFrom } from './tableConfig'

describe('tableConfigFrom', () => {
  it('reads per-column formats', () => {
    const cfg = tableConfigFrom({ table: { columns: [{ name: 'revenue', format: { style: 'currency', currency: 'USD' } }] } })
    expect(cfg.columns.revenue).toEqual({ style: 'currency', currency: 'USD' })
  })
  it('returns an empty map for missing config', () => {
    expect(tableConfigFrom(undefined).columns).toEqual({})
  })
  it('ignores malformed entries', () => {
    const cfg = tableConfigFrom({
      table: {
        columns: [
          null,
          'nope',
          { format: { style: 'number' } },
          { name: '', format: { style: 'number' } },
          { name: 'not-object', format: 'currency' },
          { name: 'unknown-style', format: { style: 'date' } },
          { name: 'ok', format: { style: 'percent', decimals: 1 } },
        ],
      },
    })
    expect(cfg.columns).toEqual({ ok: { style: 'percent', decimals: 1 } })
  })
  it('returns an empty map when table or columns has the wrong shape', () => {
    expect(tableConfigFrom({ table: 'x' }).columns).toEqual({})
    expect(tableConfigFrom({ table: { columns: {} } }).columns).toEqual({})
    expect(tableConfigFrom({ table: { columns: [{ name: 'x', format: {} }] } }).columns).toEqual({})
  })
  it('keeps prototype-named columns as own entries', () => {
    const cfg = tableConfigFrom({ table: { columns: [{ name: '__proto__', format: { style: 'number' } }] } })
    expect(Object.hasOwn(cfg.columns, '__proto__')).toBe(true)
    expect(cfg.columns['__proto__']).toEqual({ style: 'number' })
  })
})
