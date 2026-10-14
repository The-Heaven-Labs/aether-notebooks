import { describe, test, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { HeadersEditor, headersToRows, rowsToPayload, REDACTED_SENTINEL, type HeaderRow } from './HeadersEditor'

describe('HeadersEditor', () => {
  test('renders rows and edits values', () => {
    const onChange = vi.fn()
    render(<HeadersEditor rows={[{ key: 'X-Title', value: 'Aether', sensitive: false }]} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('Header 1 value'), { target: { value: 'Aether 2' } })
    expect(onChange).toHaveBeenCalledWith([{ key: 'X-Title', value: 'Aether 2', sensitive: false }])
  })

  test('masks sensitive values that are redacted on the server', () => {
    render(<HeadersEditor rows={[{ key: 'X-Api-Key', value: REDACTED_SENTINEL, sensitive: true }]} onChange={vi.fn()} />)
    const input = screen.getByLabelText('Header 1 value') as HTMLInputElement
    expect(input.type).toBe('password')
    expect(input.value).toBe('')
  })

  test('adds and removes rows', () => {
    const onChange = vi.fn()
    render(<HeadersEditor rows={[{ key: 'A', value: '1', sensitive: false }]} onChange={onChange} />)
    fireEvent.click(screen.getByText('+ Add Header') as HTMLElement)
    expect(onChange).toHaveBeenCalledWith([
      { key: 'A', value: '1', sensitive: false },
      { key: '', value: '', sensitive: false },
    ])
    fireEvent.click(screen.getByLabelText('Remove header 1'))
    expect(onChange).toHaveBeenCalledWith([])
  })

  test('rowsToPayload round-trips the sentinel for unchanged secrets', () => {
    const payload = rowsToPayload([
      { key: 'X-Title', value: 'Aether', sensitive: false },
      { key: 'X-Api-Key', value: REDACTED_SENTINEL, sensitive: true },
      { key: '', value: 'ignored', sensitive: false },
    ])
    expect(payload.headers).toEqual({ 'X-Title': 'Aether', 'X-Api-Key': REDACTED_SENTINEL })
    expect(payload.secrets).toEqual(['X-Api-Key'])
  })

  test('headersToRows maps API payloads and defaults to one empty row', () => {
    expect(headersToRows({ 'X-Api-Key': 'v' }, ['X-Api-Key'])).toEqual([
      { key: 'X-Api-Key', value: 'v', sensitive: true },
    ])
    expect(headersToRows(undefined, undefined)).toEqual([{ key: '', value: '', sensitive: false }])
  })

  test('headersToRows matches secret names case-insensitively', () => {
    expect(headersToRows({ 'x-api-key': 'v' }, ['X-API-Key'])).toEqual([
      { key: 'x-api-key', value: 'v', sensitive: true },
    ])
  })

  test('typing into a masked field replaces the sentinel', () => {
    const onChange = vi.fn()
    render(<HeadersEditor rows={[{ key: 'X-Api-Key', value: REDACTED_SENTINEL, sensitive: true }]} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('Header 1 value'), { target: { value: 'fresh' } })
    expect(rowsToPayload(onChange.mock.calls.at(-1)![0] as HeaderRow[]).headers).toEqual({ 'X-Api-Key': 'fresh' })
  })

  test('unchecking sensitive clears the value', () => {
    const onChange = vi.fn()
    render(<HeadersEditor rows={[{ key: 'X-Api-Key', value: REDACTED_SENTINEL, sensitive: true }]} onChange={onChange} />)
    fireEvent.click(screen.getByRole('checkbox'))
    expect(onChange).toHaveBeenCalledWith([{ key: 'X-Api-Key', value: '', sensitive: false }])
  })
})
