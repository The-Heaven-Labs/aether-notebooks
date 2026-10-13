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

  test('Home and End jump to the first and last tabs', () => {
    const onChange = vi.fn()
    render(<Harness initial="orders" onChange={onChange} />)

    tab('Orders').focus()
    fireEvent.keyDown(tab('Orders'), { key: 'End' })
    expect(onChange).toHaveBeenCalledWith('funnel')
    expect(tab('Funnel')).toHaveAttribute('aria-selected', 'true')
    expect(tab('Funnel')).toHaveFocus()

    fireEvent.keyDown(tab('Funnel'), { key: 'Home' })
    expect(onChange).toHaveBeenCalledWith('overview')
    expect(tab('Overview')).toHaveAttribute('aria-selected', 'true')
    expect(tab('Overview')).toHaveFocus()
  })

  test('links each tab to its chapter panel', () => {
    render(<AppTabs tabs={TABS} activeId="overview" onChange={() => {}} />)

    expect(tab('Orders')).toHaveAttribute('id', 'app-tab-orders')
    expect(tab('Orders')).toHaveAttribute('aria-controls', 'app-tabpanel-orders')
  })

  test('draws the active indicator inside the tab box without vertical overflow', () => {
    render(<Harness initial="overview" />)

    // One-axis `auto` computes the other axis to `auto` too; pinning both
    // keeps Chrome from showing a scrollbar for any indicator bleed.
    expect(screen.getByRole('tablist').style.overflowY).toBe('hidden')
    expect(tab('Overview').style.boxShadow).toBe('inset 0 -1px 0 0 var(--accent)')
    expect(tab('Orders').style.boxShadow).toBe('')

    fireEvent.click(tab('Orders'))

    // The indicator moves with selection and stays inside the box (no
    // negative-margin overlay clipped by the scroll container).
    expect(tab('Overview').style.boxShadow).toBe('')
    expect(tab('Orders').style.boxShadow).toBe('inset 0 -1px 0 0 var(--accent)')
    for (const t of screen.getAllByRole('tab')) {
      expect(t.style.marginBottom).toBe('')
    }
  })
})
