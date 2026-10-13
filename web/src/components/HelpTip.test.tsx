import { describe, test, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { HelpTip } from './HelpTip'

const HELP = {
  label: 'Value column',
  hint: 'The numeric column the metric computes over.',
  example: 'revenue',
}

describe('HelpTip', () => {
  test('renders a focusable trigger and opens the popover on click', () => {
    render(<HelpTip help={HELP} />)

    const trigger = screen.getByRole('button', { name: 'Help: Value column' })
    trigger.focus()
    expect(trigger).toHaveFocus()
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()

    fireEvent.click(trigger)
    const popover = screen.getByRole('tooltip')
    expect(popover).toHaveTextContent('The numeric column the metric computes over.')
    expect(popover).toHaveTextContent('revenue')
    // The open popover is wired to the trigger for screen readers.
    expect(trigger).toHaveAttribute('aria-describedby', popover.id)
    expect(trigger).toHaveAttribute('aria-expanded', 'true')

    fireEvent.click(trigger)
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
  })

  test('opens on Enter and closes on Escape', () => {
    render(<HelpTip help={HELP} />)
    const trigger = screen.getByRole('button', { name: 'Help: Value column' })

    fireEvent.keyDown(trigger, { key: 'Enter' })
    expect(screen.getByRole('tooltip')).toBeInTheDocument()

    fireEvent.keyDown(trigger, { key: 'Escape' })
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  test('closes on blur — no hover required', () => {
    render(<HelpTip help={HELP} />)
    const trigger = screen.getByRole('button', { name: 'Help: Value column' })

    fireEvent.click(trigger)
    expect(screen.getByRole('tooltip')).toBeInTheDocument()

    fireEvent.blur(trigger)
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })
})
