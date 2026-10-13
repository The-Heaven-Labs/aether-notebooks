import { describe, expect, test, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderWithProviders } from '../test/utils'
import { CustomWidget } from './CustomWidget'

describe('CustomWidget', () => {
  test('disabled renders the org placeholder', () => {
    renderWithProviders(<CustomWidget html="<p>x</p>" css="" js="" bindings={[]} enabled={false} widgets={[]} />)
    expect(screen.getByText('Custom code is disabled for this organization.')).toBeInTheDocument()
  })
  test('empty renders the authoring hint', () => {
    renderWithProviders(<CustomWidget html="" css="" js="" bindings={[]} widgets={[]} />)
    expect(screen.getByText(/Empty custom component/)).toBeInTheDocument()
  })
  test('renders the sandboxed frame when authored', () => {
    renderWithProviders(<CustomWidget html="<p>x</p>" css="" js="1" bindings={[]} widgets={[]} />)
    expect(screen.getByTitle('Custom component')).toHaveAttribute('sandbox', 'allow-scripts')
  })
  test('answers request_data for a binding it cannot fetch with an error frame', async () => {
    renderWithProviders(<CustomWidget
      html="<p>x</p>" css="" js="1"
      bindings={[{ key: 'sales', widget_id: 'missing' }]}
      widgets={[]}
    />)
    const frame = screen.getByTitle('Custom component') as HTMLIFrameElement
    // jsdom exposes a Window proxy for the iframe; patch its postMessage as
    // the spy the host's reply lands on.
    const push = vi.fn()
    Object.defineProperty(frame.contentWindow!, 'postMessage', { value: push, configurable: true })

    window.dispatchEvent(new MessageEvent('message', {
      data: { type: 'request_data', key: 'sales' },
      source: frame.contentWindow,
    }))

    await waitFor(() => expect(push).toHaveBeenCalled())
    expect(push.mock.calls[0][0]).toMatchObject({ type: 'error', key: 'sales' })
    expect(push.mock.calls[0][1]).toBe('*')
  })
})
