import { useState } from 'react'
import { describe, test, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AppTabs } from './AppTabs'
import type { DashboardTab } from '../types'

const TABS: DashboardTab[] = [
  { id: 'overview', title: 'Overview' },
  { id: 'orders', title: 'Orders' },
  { id: 'funnel', title: 'Funnel' },
]

/** Controlled stand-in for the pages: selection lives in parent state. */
function Harness({ initial = 'overview', onChange }: { initial?: string; onChange?: (id: string) => void }) {
  const [active, setActive] = useState(initial)
  return (
    <AppTabs
      tabs={TABS}
      activeId={active}
      onChange={(id) => {
        setActive(id)
        onChange?.(id)
      }}
    />
  )
}

function tab(name: string): HTMLElement {
  return screen.getByRole('tab', { name })
}

describe('AppTabs', () => {
  test('renders nothing when the dashboard defines no tabs', () => {
    const { container } = render(<AppTabs tabs={[]} activeId={null} onChange={() => {}} />)
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByRole('tablist')).not.toBeInTheDocument()
  })

  test('renders one tab per definition with only the active one selected', () => {
    render(<AppTabs tabs={TABS} activeId="orders" onChange={() => {}} />)

    expect(screen.getByRole('tablist')).toBeInTheDocument()
    expect(screen.getAllByRole('tab')).toHaveLength(3)
    expect(tab('Orders')).toHaveAttribute('aria-selected', 'true')
    expect(tab('Overview')).toHaveAttribute('aria-selected', 'false')
    expect(tab('Funnel')).toHaveAttribute('aria-selected', 'false')
    // Roving tabindex: only the selected tab is in the tab order.
    expect(tab('Orders')).toHaveAttribute('tabindex', '0')
    expect(tab('Overview')).toHaveAttribute('tabindex', '-1')
  })

  test('clicking a tab reports it through onChange', () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)

    fireEvent.click(tab('Funnel'))

    expect(onChange).toHaveBeenCalledWith('funnel')
    expect(tab('Funnel')).toHaveAttribute('aria-selected', 'true')
  })

  test('ArrowRight selects the next tab, wrapping at the end', () => {
    const onChange = vi.fn()
    render(<Harness initial="funnel" onChange={onChange} />)

    tab('Funnel').focus()
    fireEvent.keyDown(tab('Funnel'), { key: 'ArrowRight' })

    expect(onChange).toHaveBeenCalledWith('overview')
    expect(tab('Overview')).toHaveAttribute('aria-selected', 'true')
    // Automatic activation: focus follows selection.
    expect(tab('Overview')).toHaveFocus()
  })

  test('ArrowLeft selects the previous tab, wrapping at the start', () => {
    const onChange = vi.fn()
    render(<Harness initial="overview" onChange={onChange} />)

    tab('Overview').focus()
    fireEvent.keyDown(tab('Overview'), { key: 'ArrowLeft' })

    expect(onChange).toHaveBeenCalledWith('funnel')
    expect(tab('Funnel')).toHaveAttribute('aria-selected', 'true')
    expect(tab('Funnel')).toHaveFocus()
  })

  test('the previously selected tab drops its accent underline', () => {
    render(<Harness initial="overview" />)

    expect(tab('Overview').style.borderBottomColor).toBe('var(--accent)')
    expect(tab('Orders').style.borderBottomColor).toBe('transparent')

    fireEvent.click(tab('Orders'))

    // Regression guard: a shorthand-set border color cleared by React's style
    // diff would fall back to currentcolor and draw a second underline.
    expect(tab('Overview').style.borderBottomColor).toBe('transparent')
    expect(tab('Orders').style.borderBottomColor).toBe('var(--accent)')
  })
})
