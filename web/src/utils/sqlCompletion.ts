import type { ConnectorSchema, SchemaColumn } from '../types'

/**
 * Builds a CodeMirror SQL autocompletion schema map from a connector catalog.
 * Keys are table names (and `schema.name` when a schema is present); values are
 * the table's column names. Duplicate names across schemas resolve to the last
 * catalog entry; CodeMirror will offer completions for every key regardless.
 */
export function buildSqlCompletionSchema(catalog: ConnectorSchema): Record<string, readonly string[]> {
  const schema: Record<string, string[]> = {}
  for (const table of catalog.tables) {
    const columns = table.columns.map((c: SchemaColumn) => c.name)
    schema[table.name] = columns
    if (table.schema) {
      schema[`${table.schema}.${table.name}`] = columns
    }
  }
  return schema
}
