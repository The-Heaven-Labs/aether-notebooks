import { createRef } from 'react'
import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/react'
import { WidgetFrame } from './WidgetFrame'

describe('WidgetFrame', () => {
  it('renders a bordered card by default', () => {
    const { container } = render(<WidgetFrame appearance={{ background: 'card', border: true, padding: 'md', align: 'left' }}>x</WidgetFrame>)
    const el = container.firstElementChild as HTMLElement
    expect(el.style.background).toBe('var(--bg-card)')
    expect(el.style.border).not.toBe('none')
  })
  it('transparent removes background and border', () => {
    const { container } = render(<WidgetFrame appearance={{ background: 'transparent', border: true, padding: 'none', align: 'left' }}>x</WidgetFrame>)
    const el = container.firstElementChild as HTMLElement
    expect(el.style.background).toBe('transparent')
    // jsdom 30 normalizes the `border: none` shorthand to `medium` (the
    // initial width) on readback, so the longhand is the reliable signal.
    expect(el.style.borderStyle).toBe('none')
  })

  it('maps padding none, sm and md to outer padding', () => {
    const cases: Array<[('none' | 'sm' | 'md'), string]> = [['none', '0px'], ['sm', '8px'], ['md', '16px']]
    for (const [padding, expected] of cases) {
      const { container } = render(<WidgetFrame appearance={{ background: 'card', border: true, padding, align: 'left' }}>x</WidgetFrame>)
      expect((container.firstElementChild as HTMLElement).style.padding).toBe(expected)
    }
  })

  it('maps align to textAlign', () => {
    const cases = ['left', 'center', 'right'] as const
    for (const align of cases) {
      const { container } = render(<WidgetFrame appearance={{ background: 'card', border: true, padding: 'none', align }}>x</WidgetFrame>)
      expect((container.firstElementChild as HTMLElement).style.textAlign).toBe(align)
    }
  })

  it('drops the border on a card when border is false', () => {
    const { container } = render(<WidgetFrame appearance={{ background: 'card', border: false, padding: 'none', align: 'left' }}>x</WidgetFrame>)
    expect((container.firstElementChild as HTMLElement).style.borderStyle).toBe('none')
  })

  it('keeps the frame structure of the widget card and renders children', () => {
    const { container, getByText } = render(<WidgetFrame appearance={{ background: 'card', border: true, padding: 'none', align: 'left' }}>content</WidgetFrame>)
    const el = container.firstElementChild as HTMLElement
    expect(el.style.height).toBe('100%')
    expect(el.style.display).toBe('flex')
    expect(el.style.flexDirection).toBe('column')
    expect(el.style.overflow).toBe('hidden')
    expect(el.style.borderRadius).toBe('4px')
    expect(getByText('content')).toBeTruthy()
  })

  it('passes through ref, className and style for layout hosts', () => {
    const ref = createRef<HTMLDivElement>()
    const { container } = render(
      <WidgetFrame appearance={{ background: 'card', border: true, padding: 'none', align: 'left' }} ref={ref} className="react-grid-item" style={{ position: 'absolute', background: 'red' }}>
        x
      </WidgetFrame>,
    )
    const el = container.firstElementChild as HTMLElement
    expect(ref.current).toBe(el)
    expect(el.className).toBe('react-grid-item')
    expect(el.style.position).toBe('absolute')
    // Host styles win so grid positioning is never overridden by the frame.
    expect(el.style.background).toBe('red')
  })
})
