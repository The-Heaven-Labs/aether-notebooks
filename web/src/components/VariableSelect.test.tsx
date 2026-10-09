import { describe, test, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { VariableSelect } from './VariableSelect'

const opts = (n: number) =>
  Array.from({ length: n }, (_, i) => ({ label: `Option ${i + 1}`, value: `v${i + 1}` }))

describe('VariableSelect', () => {
  test('single select shows the empty label until a value is chosen', () => {
    const onChange = vi.fn()
    render(<VariableSelect id="v" options={opts(3)} value="" onChange={onChange} ariaLabel="v" />)
    expect(screen.getByLabelText('v')).toHaveTextContent('All')
    fireEvent.click(screen.getByLabelText('v'))
    fireEvent.click(screen.getByRole('option', { name: 'Option 2' }))
    expect(onChange).toHaveBeenCalledWith('v2')
  })

  test('multi select summarizes the selection and stays open while toggling', () => {
    const onChange = vi.fn()
    const options = opts(5)
    const { rerender } = render(
      <VariableSelect id="v" multiple options={options} value={[]} onChange={onChange} ariaLabel="Picks" />,
    )
    fireEvent.click(screen.getByLabelText('Picks'))
    fireEvent.click(screen.getByRole('option', { name: 'Option 1' }))
    expect(onChange).toHaveBeenLastCalledWith(['v1'])
    // The list stays open for the next toggle.
    expect(screen.getByRole('listbox')).toBeInTheDocument()

    rerender(<VariableSelect id="v" multiple options={options} value={['v1', 'v2', 'v3']} onChange={onChange} ariaLabel="Picks" />)
    expect(screen.getByLabelText('Picks')).toHaveTextContent('Option 1, Option 2, Option 3')
    rerender(<VariableSelect id="v" multiple options={options} value={['v1', 'v2', 'v3', 'v4']} onChange={onChange} ariaLabel="Picks" />)
    expect(screen.getByLabelText('Picks')).toHaveTextContent('4 of 5 selected')
    rerender(<VariableSelect id="v" multiple options={options} value={['v1', 'v2', 'v3', 'v4', 'v5']} onChange={onChange} ariaLabel="Picks" />)
    expect(screen.getByLabelText('Picks')).toHaveTextContent('All')
    rerender(<VariableSelect id="v" multiple options={options} value={[]} onChange={onChange} ariaLabel="Picks" />)
    expect(screen.getByLabelText('Picks')).toHaveTextContent('None')
  })

  test('All and Clear act on every option', () => {
    const onChange = vi.fn()
    render(<VariableSelect id="v" multiple options={opts(10)} value={[]} onChange={onChange} ariaLabel="Big" />)
    fireEvent.click(screen.getByLabelText('Big'))
    fireEvent.click(screen.getByRole('button', { name: 'All' }))
    expect(onChange).toHaveBeenLastCalledWith(opts(10).map(o => o.value))
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    expect(onChange).toHaveBeenLastCalledWith([])
  })

  test('large option sets get a search field that filters', () => {
    render(<VariableSelect id="v" options={opts(10)} value="" onChange={() => {}} ariaLabel="v" />)
    fireEvent.click(screen.getByLabelText('v'))
    fireEvent.change(screen.getByLabelText('Search v'), { target: { value: 'Option 10' } })
    expect(screen.getByRole('option', { name: 'Option 10' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'Option 1' })).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Search v'), { target: { value: 'zzz' } })
    expect(screen.getByText('No matching options')).toBeInTheDocument()
  })

  test('small option sets skip the search field', () => {
    render(<VariableSelect id="v" options={opts(3)} value="" onChange={() => {}} ariaLabel="v" />)
    fireEvent.click(screen.getByLabelText('v'))
    expect(screen.queryByLabelText('Search options')).not.toBeInTheDocument()
  })
})
