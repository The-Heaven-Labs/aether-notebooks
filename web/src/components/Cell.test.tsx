import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { ResizableImage, Cell } from './Cell'
import type { Cell as CellType } from '../types'

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeMarkdownCell(source = 'hello'): CellType {
  return {
    id: 'cell-1',
    notebook_id: 'nb-1',
    type: 'text',
    language: 'markdown',
    source,
    outputs: [],
    position: 0,
    created_at: '',
    updated_at: '',
    source_visible: true,
    cell_collapsed: false,
  }
}

function renderMarkdownCell(source = 'hello') {
  const cell = makeMarkdownCell(source)
  const onSourceChange = vi.fn()
  const onSave = vi.fn()
  const utils = render(
    <Cell
      cell={cell}
      connectors={[]}
      notebookId="nb-1"
      onRun={vi.fn()}
      onDelete={vi.fn()}
      onSourceChange={onSourceChange}
      onSave={onSave}
      onAssignConnector={vi.fn()}
    />
  )
  return { ...utils, onSourceChange, onSave }
}

// ── ResizableImage ─────────────────────────────────────────────────────────────

describe('ResizableImage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ blob: async () => new Blob(['x'], { type: 'image/png' }) }))
    vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:mock'), revokeObjectURL: vi.fn() })
  })
  afterEach(() => { vi.unstubAllGlobals() })

  it('renders an img with a blob url and correct alt', async () => {
    render(<ResizableImage src="/api/v1/attachments/abc123" alt="photo.png" width="100%" />)
    const img = screen.getByRole('img')
    await waitFor(() => expect(img).toHaveAttribute('src', 'blob:mock'))
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

// ── Markdown image upload ──────────────────────────────────────────────────────

describe('MarkdownView image upload', () => {
  beforeEach(() => {
    // Provide a token so getToken() returns non-null
    localStorage.setItem('hnb_token', 'test-token')
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    localStorage.removeItem('hnb_token')
    vi.unstubAllGlobals()
  })

  it('paste of an image file uploads and inserts <img> tag in the textarea', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'att-42', filename: 'screenshot.png' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { onSourceChange } = renderMarkdownCell('before ')

    // Enter edit mode by clicking rendered area
    fireEvent.click(screen.getByText('before'))
    const textarea = screen.getByPlaceholderText('Write markdown…') as HTMLTextAreaElement

    // Position cursor at end
    textarea.setSelectionRange(7, 7)

    const imageFile = new File(['(binary)'], 'screenshot.png', { type: 'image/png' })
    const clipboardData = {
      files: [imageFile],
    }

    await act(async () => {
      fireEvent.paste(textarea, { clipboardData })
    })

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1)
    })

    const [url, opts] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/v1/notebooks/nb-1/attachments')
    expect(opts.method).toBe('POST')
    expect(opts.headers.Authorization).toBe('Bearer test-token')
    expect(opts.body).toBeInstanceOf(FormData)

    await waitFor(() => {
      expect(onSourceChange).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('<img src="/api/v1/attachments/att-42" alt="screenshot.png" width="100%">')
      )
    })
  })

  it('upload button opens file input and on file select uploads and inserts <img> tag', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'att-99', filename: 'photo.jpg' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { container, onSourceChange } = renderMarkdownCell('')

    // Enter edit mode by clicking the empty block div
    const emptyBlock = container.querySelector('[data-testid="md-empty-block"]') as HTMLElement
    fireEvent.click(emptyBlock)
    const textarea = screen.getByPlaceholderText('Write markdown…') as HTMLTextAreaElement
    expect(textarea).toBeTruthy()

    // Find file input (hidden)
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    expect(fileInput).not.toBeNull()

    const imageFile = new File(['(binary)'], 'photo.jpg', { type: 'image/jpeg' })
    Object.defineProperty(fileInput, 'files', { value: [imageFile], configurable: true })

    await act(async () => {
      fireEvent.change(fileInput)
    })

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1)
    })

    const [url] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/v1/notebooks/nb-1/attachments')

    await waitFor(() => {
      expect(onSourceChange).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('<img src="/api/v1/attachments/att-99" alt="photo.jpg" width="100%">')
      )
    })
  })

  it('upload button is disabled while upload is in progress', async () => {
    // Make fetch hang until we resolve it
    let resolveUpload!: (v: unknown) => void
    const fetchMock = vi.fn().mockReturnValue(
      new Promise((resolve) => { resolveUpload = resolve })
    )
    vi.stubGlobal('fetch', fetchMock)

    const { container } = renderMarkdownCell('')

    // Enter edit mode by clicking the empty block div
    const emptyBlock = container.querySelector('[data-testid="md-empty-block"]') as HTMLElement
    fireEvent.click(emptyBlock)

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    const imageFile = new File(['(binary)'], 'loading.png', { type: 'image/png' })
    Object.defineProperty(fileInput, 'files', { value: [imageFile], configurable: true })

    // Start the upload (don't await)
    act(() => {
      fireEvent.change(fileInput)
    })

    // While fetch is pending, button should be disabled and show '...'
    await waitFor(() => {
      const btn = document.querySelector('button[title="Upload image"]') as HTMLButtonElement
      expect(btn).not.toBeNull()
      expect(btn.disabled).toBe(true)
      expect(btn.textContent).toBe('...')
    })

    // Resolve the upload
    await act(async () => {
      resolveUpload({
        ok: true,
        json: async () => ({ id: 'att-1', filename: 'loading.png' }),
      })
    })

    // After completion button should be enabled again
    await waitFor(() => {
      const btn = document.querySelector('button[title="Upload image"]') as HTMLButtonElement
      expect(btn.disabled).toBe(false)
    })
  })
})
