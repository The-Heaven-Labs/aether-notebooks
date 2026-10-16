import { describe, it, expect } from 'vitest'
import { buildSqlCompletionSchema } from './sqlCompletion'
import type { ConnectorSchema } from '../types'

function makeCatalog(...tables: ConnectorSchema['tables']): ConnectorSchema {
  return { tables }
}

describe('buildSqlCompletionSchema', () => {
  it('maps bare table names to their columns', () => {
    const catalog = makeCatalog(
      { schema: '', name: 'users', columns: [{ name: 'id', type: 'Int64' }, { name: 'email', type: 'String' }] },
      { schema: '', name: 'orders', columns: [{ name: 'id', type: 'Int64' }, { name: 'user_id', type: 'Int64' }] },
    )
    expect(buildSqlCompletionSchema(catalog)).toEqual({
      users: ['id', 'email'],
      orders: ['id', 'user_id'],
    })
  })

  it('adds qualified names when a schema is present', () => {
    const catalog = makeCatalog(
      { schema: 'analytics', name: 'events', columns: [{ name: 'ts', type: 'DateTime' }] },
    )
    expect(buildSqlCompletionSchema(catalog)).toEqual({
      events: ['ts'],
      'analytics.events': ['ts'],
    })
  })

  it('keeps the last value for duplicate names across schemas', () => {
    const catalog = makeCatalog(
      { schema: 'a', name: 'users', columns: [{ name: 'id', type: 'Int64' }] },
      { schema: 'b', name: 'users', columns: [{ name: 'uuid', type: 'UUID' }] },
    )
    const schema = buildSqlCompletionSchema(catalog)
    expect(schema.users).toEqual(['uuid'])
    expect(schema['a.users']).toEqual(['id'])
    expect(schema['b.users']).toEqual(['uuid'])
  })

  it('allows empty catalogs and empty column lists', () => {
    expect(buildSqlCompletionSchema({ tables: [] })).toEqual({})
    expect(buildSqlCompletionSchema({ tables: [{ schema: '', name: 'empty', columns: [] }] })).toEqual({
      empty: [],
    })
  })
})
