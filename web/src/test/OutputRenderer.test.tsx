import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react'
import { OutputRenderer, isAnyDetailActive } from '../components/OutputRenderer'
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

// ── TableOutput virtualization ────────────────────────────────────────────────

describe('TableOutput virtualization', () => {
  // jsdom reports zero element sizes, which makes @tanstack/react-virtual treat
  // the viewport as empty and render no rows. Give elements a real size so the
  // virtual window is non-empty and the DOM-bounding behaviour is observable.
  beforeEach(() => {
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get: () => 300 })
    Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get: () => 800 })
  })
  afterEach(() => {
    delete (HTMLElement.prototype as { offsetHeight?: unknown }).offsetHeight
    delete (HTMLElement.prototype as { offsetWidth?: unknown }).offsetWidth
  })

  function makeBigOutput(rows: number, cols = 1): Output {
    const columns = Array.from({ length: cols }, (_, c) => ({ name: `col${c}`, type: 'string' }))
    const data = Array.from({ length: rows }, (_, r) => Array.from({ length: cols }, (_, c) => `r${r}c${c}`))
    return { type: 'table', data: { columns, rows: data } }
  }

  function getRenderedRows(container: HTMLElement): HTMLElement[] {
    return Array.from(container.querySelectorAll('tbody tr'))
  }

  it('renders the full result set logically with no "Load more rows" button', async () => {
    const { container } = render(<OutputRenderer outputs={[makeBigOutput(2000)]} />)
    await waitFor(() => expect(getRenderedRows(container).length).toBeGreaterThan(0))
    expect(screen.queryByText(/Load more rows/)).toBeNull()
  })

  it('keeps the DOM bounded regardless of result size', async () => {
    const { container } = render(<OutputRenderer outputs={[makeBigOutput(2000)]} />)
    await waitFor(() => expect(getRenderedRows(container).length).toBeGreaterThan(0))
    // Only the visible window + overscan render, not all 2000 rows.
    expect(getRenderedRows(container).length).toBeLessThan(50)
    // A spacer sized to the full result set keeps the whole table scrollable.
    expect(container.querySelector('tbody')?.getAttribute('style')).toContain('height: 64000px')
  })

  it('windows wide result sets horizontally', async () => {
    const { container } = render(<OutputRenderer outputs={[makeBigOutput(10, 60)]} />)
    await waitFor(() => expect(getRenderedRows(container).length).toBeGreaterThan(0))
    const cellCount = container.querySelector('tbody tr')?.querySelectorAll('td').length ?? 0
    expect(cellCount).toBeGreaterThan(1)
    expect(cellCount).toBeLessThan(30)
  })

  it('shows the full cell value with no JS truncation', async () => {
    const longValue = 'very-long-value-'.repeat(20)
    expect(longValue.length).toBeGreaterThan(100)
    const { container } = render(
      <OutputRenderer outputs={[{ type: 'table', data: { columns: [{ name: 'val', type: 'string' }], rows: [[longValue]] } }]} />
    )
    await waitFor(() => expect(getRenderedRows(container).length).toBeGreaterThan(0))
    const td = container.querySelector('td[data-row="0"][data-col="0"]')!
    expect(td.textContent).toBe(longValue)
    expect(td.textContent).not.toContain('…')
  })

  it('re-windows the rendered rows when the table is scrolled', async () => {
    const { container } = render(<OutputRenderer outputs={[makeBigOutput(2000)]} />)
    await waitFor(() => expect(getRenderedRows(container).length).toBeGreaterThan(0))
    const area = container.querySelector('.output-scroll-area')!
    act(() => {
      area.scrollTop = 1000
      area.dispatchEvent(new Event('scroll'))
    })
    await waitFor(() => {
      const first = container.querySelector('tbody tr td')?.textContent
      expect(Number(first)).toBeGreaterThan(10)
    })
  })

  it('keeps the sticky header painted above the virtualized rows', async () => {
    const { container } = render(<OutputRenderer outputs={[makeBigOutput(50, 3)]} />)
    await waitFor(() => expect(getRenderedRows(container).length).toBeGreaterThan(0))
    const th = container.querySelector('thead th')!
    const styles = getComputedStyle(th)
    // The virtual rows are absolutely positioned + transformed (stacking
    // contexts). Without an explicit z-index on the sticky header, rows paint
    // over it and the header becomes unreadable while scrolling.
    expect(styles.position).toBe('sticky')
    expect(styles.zIndex).toBe('2')
  })

  it('opens the detail panel with the full value on click', async () => {
    const longValue = 'payload-'.repeat(40)
    const { container } = render(
      <OutputRenderer
        outputs={[{ type: 'table', data: { columns: [{ name: 'val', type: 'string' }], rows: [[longValue]] } }]}
        cellId="cell-1"
      />
    )
    await waitFor(() => expect(getRenderedRows(container).length).toBeGreaterThan(0))
    const td = container.querySelector('td[data-row="0"][data-col="0"]')!
    fireEvent.click(td)
    await waitFor(() => expect(screen.getByLabelText('Copy value')).toBeDefined())
    const pre = container.querySelector('pre')
    expect(pre?.textContent).toBe(longValue)
  })
})

// ── Detail panel copy behavior ────────────────────────────────────────────────

describe('TableOutput detail copy', () => {
  const fullValue = 'detail-value-'.repeat(4)

  beforeEach(() => {
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get: () => 300 })
    Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get: () => 800 })
  })
  afterEach(() => {
    delete (HTMLElement.prototype as { offsetHeight?: unknown }).offsetHeight
    delete (HTMLElement.prototype as { offsetWidth?: unknown }).offsetWidth
    delete (navigator as { clipboard?: unknown }).clipboard
    window.getSelection()?.removeAllRanges()
  })

  function renderOpenDetail(cellId: string) {
    const { container, unmount } = render(
      <OutputRenderer
        outputs={[{ type: 'table', data: { columns: [{ name: 'val', type: 'string' }], rows: [[fullValue]] } }]}
        cellId={cellId}
      />
    )
    const td = container.querySelector('td[data-row="0"][data-col="0"]')!
    fireEvent.click(td)
    return { container, unmount }
  }

  function stubClipboardWriteText() {
    const writeText = vi.fn(() => Promise.resolve())
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    return writeText
  }

  function dispatchCtrlC(): KeyboardEvent {
    const event = new KeyboardEvent('keydown', { key: 'c', ctrlKey: true, cancelable: true, bubbles: true })
    window.dispatchEvent(event)
    return event
  }

  function dispatchCopy(target: EventTarget = document): { event: Event; data: Record<string, string> } {
    const data: Record<string, string> = {}
    const event = new Event('copy', { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'clipboardData', {
      value: { setData: (k: string, v: string) => { data[k] = v } },
    })
    target.dispatchEvent(event)
    return { event, data }
  }

  it('copies the user selection instead of the whole value', async () => {
    const { container } = renderOpenDetail('cell-copy-selection')
    await waitFor(() => expect(screen.getByLabelText('Copy value')).toBeDefined())
    const pre = container.querySelector('pre')!
    const sel = window.getSelection()!
    const range = document.createRange()
    range.selectNodeContents(pre.firstChild!)
    sel.removeAllRanges()
    sel.addRange(range)
    expect(sel.isCollapsed).toBe(false)
    expect(sel.toString()).toBe(fullValue)

    const { event, data } = dispatchCopy()
    expect(event.defaultPrevented).toBe(false)
    expect(data['text/plain']).toBeUndefined()
  })

  it('copies the whole value when nothing is selected', async () => {
    renderOpenDetail('cell-copy-whole')
    await waitFor(() => expect(screen.getByLabelText('Copy value')).toBeDefined())
    window.getSelection()!.removeAllRanges()

    const { event, data } = dispatchCopy()
    expect(event.defaultPrevented).toBe(true)
    expect(data['text/plain']).toBe(fullValue)
  })

  it('resets the global detail state when the open panel unmounts', async () => {
    const { unmount } = renderOpenDetail('cell-unmount-global')
    await waitFor(() => expect(screen.getByLabelText('Copy value')).toBeDefined())
    expect(isAnyDetailActive()).toBe(true)
    unmount()
    expect(isAnyDetailActive()).toBe(false)
  })

  it('does not intercept Ctrl+C keydown while a selection exists', async () => {
    const writeText = stubClipboardWriteText()
    const { container } = renderOpenDetail('cell-keydown-selection')
    await waitFor(() => expect(screen.getByLabelText('Copy value')).toBeDefined())
    const pre = container.querySelector('pre')!
    const sel = window.getSelection()!
    const range = document.createRange()
    range.selectNodeContents(pre.firstChild!)
    sel.removeAllRanges()
    sel.addRange(range)
    expect(sel.isCollapsed).toBe(false)
    expect(sel.toString()).toBe(fullValue)

    const event = dispatchCtrlC()
    expect(event.defaultPrevented).toBe(false)
    expect(writeText).not.toHaveBeenCalled()
  })

  it('does not write the whole value on Ctrl+C keydown with no selection', async () => {
    const writeText = stubClipboardWriteText()
    renderOpenDetail('cell-keydown-no-selection')
    await waitFor(() => expect(screen.getByLabelText('Copy value')).toBeDefined())
    window.getSelection()!.removeAllRanges()

    const event = dispatchCtrlC()
    expect(event.defaultPrevented).toBe(false)
    expect(writeText).not.toHaveBeenCalled()
  })
})

// ── Output truncation & streaming download ───────────────────────────────────

describe('OutputRenderer truncation', () => {
  beforeEach(() => {
    localStorage.setItem('aether_token', 'test-token')
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get: () => 300 })
    Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get: () => 800 })
  })
  afterEach(() => {
    localStorage.removeItem('aether_token')
    delete (HTMLElement.prototype as { offsetHeight?: unknown }).offsetHeight
    delete (HTMLElement.prototype as { offsetWidth?: unknown }).offsetWidth
  })

  it('shows the truncation badge and a full-result download link on an executor-truncated table', async () => {
    const truncated: Output = {
      type: 'table',
      data: {
        columns: [{ name: 'val', type: 'string' }],
        rows: [['a'], ['b']],
        truncated: true,
        rows_included: 2,
        rows_total: -1,
        bytes: 5120,
      },
    }
    render(<OutputRenderer outputs={[truncated]} cellId="cell-42" />)
    await waitFor(() => expect(screen.getByText(/Truncated — 2 .*rows \/ 5\.0 KB/)).toBeDefined())
    const fullLink = screen.getByLabelText('Download full result')
    expect(fullLink.getAttribute('href')).toBe('/api/v1/cells/cell-42/outputs/download?token=test-token')
  })

  it('shows the badge with a known total row count when the driver reports it', async () => {
    const truncated: Output = {
      type: 'table',
      data: {
        columns: [{ name: 'val', type: 'string' }],
        rows: [['a'], ['b']],
        truncated: true,
        rows_included: 2,
        rows_total: 26573,
        bytes: 10485760,
      },
    }
    render(<OutputRenderer outputs={[truncated]} cellId="cell-43" />)
    await waitFor(() => expect(screen.getByText(/Truncated — 2 of 26573 rows \/ 10\.0 MB/)).toBeDefined())
  })

  it('renders the read-path stub with a download button when outputs were not inlined', async () => {
    const stub: Output = { type: 'table', data: { truncated: true, bytes: 149600000 } }
    render(<OutputRenderer outputs={[stub]} cellId="cell-44" />)
    await waitFor(() => expect(screen.getByText(/Output truncated — 142\.7 MB not inlined/)).toBeDefined())
    const downloadLink = screen.getByLabelText('Download full result')
    expect(downloadLink.getAttribute('href')).toBe('/api/v1/cells/cell-44/outputs/download?token=test-token')
  })

  it('hides download buttons when data export is disabled org-wide', async () => {
    const truncated: Output = {
      type: 'table',
      data: {
        columns: [{ name: 'val', type: 'string' }],
        rows: [['a']],
        truncated: true,
        rows_included: 1,
        rows_total: 5,
        bytes: 1024,
      },
    }
    render(<OutputRenderer outputs={[truncated]} cellId="cell-45" hideExport />)
    await waitFor(() => expect(screen.getByText(/Truncated — 1 of 5 rows \/ 1\.0 KB/)).toBeDefined())
    expect(screen.queryByLabelText('Download full result')).toBeNull()
    expect(screen.queryByLabelText('Download as CSV')).toBeNull()
  })
})
