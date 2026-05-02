import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { OutputRenderer } from '../components/OutputRenderer'
import type { Output } from '../types'

const makeTableOutput = (colType: string): Output => ({
  type: 'table',
  data: {
    columns: [{ name: 'val', type: colType }],
    rows: [['test']],
  },
})

describe('OutputRenderer type icons', () => {
  it('shows # icon for integer type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('integer')]} />)
    expect(screen.getByTitle('Integer (integer)')).toBeDefined()
  })

  it('shows 0.1 icon for float type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('float')]} />)
    expect(screen.getByTitle('Float (float)')).toBeDefined()
  })

  it('shows calendar icon for date type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('date')]} />)
    expect(screen.getByTitle('Date (date)')).toBeDefined()
  })

  it('shows ? icon for unknown type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('super_weird_type')]} />)
    expect(screen.getByTitle('Unknown (super_weird_type)')).toBeDefined()
  })

  it('shows {} icon for jsonb type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('jsonb')]} />)
    expect(screen.getByTitle('JSON (jsonb)')).toBeDefined()
  })

  it('resolves LowCardinality(String) to String', () => {
    render(<OutputRenderer outputs={[makeTableOutput('LowCardinality(String)')]} />)
    expect(screen.getByTitle('String (LowCardinality(String))')).toBeDefined()
  })

  it('resolves UInt64 to Integer', () => {
    render(<OutputRenderer outputs={[makeTableOutput('UInt64')]} />)
    expect(screen.getByTitle('Integer (UInt64)')).toBeDefined()
  })

  it('resolves Decimal(10, 2) to Float', () => {
    render(<OutputRenderer outputs={[makeTableOutput('Decimal(10, 2)')]} />)
    expect(screen.getByTitle('Float (Decimal(10, 2))')).toBeDefined()
  })

  it('resolves Nullable(DateTime) to Datetime', () => {
    render(<OutputRenderer outputs={[makeTableOutput('Nullable(DateTime)')]} />)
    expect(screen.getByTitle('Datetime (Nullable(DateTime))')).toBeDefined()
  })
})
