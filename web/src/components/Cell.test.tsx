import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ResizableImage } from './Cell'

describe('ResizableImage', () => {
  it('renders an img with the correct src and alt', () => {
    render(<ResizableImage src="/api/v1/attachments/abc123" alt="photo.png" width="100%" />)
    const img = screen.getByRole('img')
    expect(img).toHaveAttribute('src', '/api/v1/attachments/abc123')
    expect(img).toHaveAttribute('alt', 'photo.png')
  })

  it('renders the resize handle', () => {
    const { container } = render(<ResizableImage src="/api/v1/attachments/abc123" alt="test" />)
    const handle = container.querySelector('.img-resize-handle')
    expect(handle).not.toBeNull()
  })

  it('calls onResize with the new pixel width after drag', () => {
    const onResize = vi.fn()
    const { container } = render(
      <ResizableImage src="/api/v1/attachments/abc123" alt="test" width="300" onResize={onResize} />
    )
    const handle = container.querySelector('.img-resize-handle')!

    fireEvent.mouseDown(handle, { clientX: 100 })
    fireEvent.mouseMove(document, { clientX: 200 })
    fireEvent.mouseUp(document, { clientX: 200 })

    expect(onResize).toHaveBeenCalledTimes(1)
    const [calledSrc, calledWidth] = onResize.mock.calls[0]
    expect(calledSrc).toBe('/api/v1/attachments/abc123')
    expect(typeof calledWidth).toBe('number')
    expect(calledWidth).toBeGreaterThan(0)
  })
})
