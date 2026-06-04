import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { ResizableImage } from './MarkdownCell'
import { Cell } from './Cell'
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
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, blob: async () => new Blob(['x'], { type: 'image/png' }) }))
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

  it('falls back to src when fetch returns non-ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 404, blob: async () => new Blob(['not found'], { type: 'text/plain' }) }))
    render(<ResizableImage src="/api/v1/attachments/missing" alt="missing" />)
    const img = screen.getByRole('img')
    await waitFor(() => expect(img).toHaveAttribute('src', '/api/v1/attachments/missing'))
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

    const { onSourceChange, onSave } = renderMarkdownCell('before ')

    // Enter edit mode by clicking the rendered paragraph (not the hidden textarea)
    fireEvent.click(screen.getByText('before', { selector: 'p' }))
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
      expect(fetchMock.mock.calls.some((args: unknown[]) => args[0] === '/api/v1/notebooks/nb-1/attachments')).toBe(true)
    })

    const uploadCall = fetchMock.mock.calls.find((args: unknown[]) => args[0] === '/api/v1/notebooks/nb-1/attachments')!
    const [url, opts] = uploadCall
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

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith(
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

    const { container, onSourceChange, onSave } = renderMarkdownCell('')

    // Enter edit mode by clicking the empty block div
    const emptyBlock = container.querySelector('[data-testid="md-empty-block"]') as HTMLElement
    fireEvent.click(emptyBlock)
    const textarea = screen.getByPlaceholderText('Write markdown…') as HTMLTextAreaElement
    expect(textarea).toBeTruthy()

    // Click the upload button to arm the file input ref
    const uploadBtn = document.querySelector('button[title="Upload image"]') as HTMLButtonElement
    expect(uploadBtn).not.toBeNull()
    fireEvent.click(uploadBtn)

    // Find file input (hidden)
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    expect(fileInput).not.toBeNull()

    const imageFile = new File(['(binary)'], 'photo.jpg', { type: 'image/jpeg' })
    Object.defineProperty(fileInput, 'files', { value: [imageFile], configurable: true })

    await act(async () => {
      fireEvent.change(fileInput)
    })

    await waitFor(() => {
      expect(fetchMock.mock.calls.some((args: unknown[]) => args[0] === '/api/v1/notebooks/nb-1/attachments')).toBe(true)
    })

    const [url] = fetchMock.mock.calls.find((args: unknown[]) => args[0] === '/api/v1/notebooks/nb-1/attachments')!
    expect(url).toBe('/api/v1/notebooks/nb-1/attachments')

    await waitFor(() => {
      expect(onSourceChange).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('<img src="/api/v1/attachments/att-99" alt="photo.jpg" width="100%">')
      )
    })

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('<img src="/api/v1/attachments/att-99" alt="photo.jpg" width="100%">')
      )
    })
  })

  it('drop of an image file uploads and inserts <img> tag', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'att-drop-1', filename: 'dropped.png' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { onSourceChange, onSave } = renderMarkdownCell('drop here')

    // Click the rendered paragraph to get the block div
    fireEvent.click(screen.getByText('drop here', { selector: 'p' }))

    // Blur to exit edit mode so we can drop on the rendered view
    const textarea = screen.getByPlaceholderText('Write markdown…') as HTMLTextAreaElement
    fireEvent.blur(textarea)

    // Find the rendered block div (the one with the paragraph)
    const blockDiv = screen.getByText('drop here', { selector: 'p' }).parentElement as HTMLElement

    const imageFile = new File(['(binary)'], 'dropped.png', { type: 'image/png' })
    const dataTransfer = {
      files: [imageFile],
    }

    await act(async () => {
      fireEvent.dragOver(blockDiv)
      fireEvent.drop(blockDiv, { dataTransfer })
    })

    await waitFor(() => {
      expect(fetchMock.mock.calls.some((args: unknown[]) => args[0] === '/api/v1/notebooks/nb-1/attachments')).toBe(true)
    })

    const uploadCall = fetchMock.mock.calls.find((args: unknown[]) => args[0] === '/api/v1/notebooks/nb-1/attachments')!
    expect(uploadCall[0]).toBe('/api/v1/notebooks/nb-1/attachments')

    await waitFor(() => {
      expect(onSourceChange).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('<img src="/api/v1/attachments/att-drop-1" alt="dropped.png" width="100%">')
      )
    })

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('<img src="/api/v1/attachments/att-drop-1" alt="dropped.png" width="100%">')
      )
    })
  })

  it('upload still works after textarea blurs before file selection', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'att-blur', filename: 'blur.png' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { container, onSourceChange, onSave } = renderMarkdownCell('')

    // Enter edit mode
    const emptyBlock = container.querySelector('[data-testid="md-empty-block"]') as HTMLElement
    fireEvent.click(emptyBlock)
    const textarea = screen.getByPlaceholderText('Write markdown…') as HTMLTextAreaElement

    // Click the upload button to arm the ref
    const uploadBtn = document.querySelector('button[title="Upload image"]') as HTMLButtonElement
    fireEvent.click(uploadBtn)

    // Grab file input before blur removes the toolbar
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    expect(fileInput).not.toBeNull()

    // Simulate the file dialog blurring the textarea
    fireEvent.blur(textarea)

    const imageFile = new File(['(binary)'], 'blur.png', { type: 'image/png' })
    Object.defineProperty(fileInput, 'files', { value: [imageFile], configurable: true })

    await act(async () => {
      fireEvent.change(fileInput)
    })

    await waitFor(() => {
      expect(fetchMock.mock.calls.some((args: unknown[]) => args[0] === '/api/v1/notebooks/nb-1/attachments')).toBe(true)
    })

    await waitFor(() => {
      expect(onSourceChange).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('<img src="/api/v1/attachments/att-blur" alt="blur.png" width="100%">')
      )
    })

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('<img src="/api/v1/attachments/att-blur" alt="blur.png" width="100%">')
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

    // Click the upload button to arm the file input ref
    const uploadBtn = document.querySelector('button[title="Upload image"]') as HTMLButtonElement
    expect(uploadBtn).not.toBeNull()
    fireEvent.click(uploadBtn)

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

// ── Accessibility: CodeMirror editor label ────────────────────────────────────

describe('CodeEditorView accessibility', () => {
  it('CodeMirror editor has an aria-label with cell title', async () => {
    const cell: CellType = {
      id: 'cell-aria',
      notebook_id: 'nb-1',
      type: 'code',
      language: 'sql',
      source: 'SELECT 1',
      outputs: [],
      position: 0,
      created_at: '',
      updated_at: '',
      source_visible: true,
      cell_collapsed: false,
      title: 'My Query',
    }
    const { container } = render(
      <Cell
        cell={cell}
        connectors={[]}
        notebookId="nb-1"
        onRun={vi.fn()}
        onDelete={vi.fn()}
        onSourceChange={vi.fn()}
        onAssignConnector={vi.fn()}
        index={2}
      />
    )
    await waitFor(() => {
      const cmContent = container.querySelector('.cm-content')
      expect(cmContent).not.toBeNull()
      expect(cmContent?.getAttribute('aria-label')).toContain('SQL editor: My Query')
    })
  })

  it('CodeMirror editor falls back to cell index when no title', async () => {
    const cell: CellType = {
      id: 'cell-notitle',
      notebook_id: 'nb-1',
      type: 'code',
      language: 'sql',
      source: 'SELECT 1',
      outputs: [],
      position: 0,
      created_at: '',
      updated_at: '',
      source_visible: true,
      cell_collapsed: false,
    }
    const { container } = render(
      <Cell
        cell={cell}
        connectors={[]}
        notebookId="nb-1"
        onRun={vi.fn()}
        onDelete={vi.fn()}
        onSourceChange={vi.fn()}
        onAssignConnector={vi.fn()}
        index={4}
      />
    )
    await waitFor(() => {
      const cmContent = container.querySelector('.cm-content')
      expect(cmContent).not.toBeNull()
      expect(cmContent?.getAttribute('aria-label')).toContain('SQL editor cell 5')
    })
  })
})

// ── Markdown image resize ─────────────────────────────────────────────────────

describe('MarkdownView image resize', () => {
  beforeEach(() => {
    localStorage.setItem('hnb_token', 'test-token')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, blob: async () => new Blob(['x'], { type: 'image/png' }) }))
    vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:mock'), revokeObjectURL: vi.fn() })
  })

  afterEach(() => {
    localStorage.removeItem('hnb_token')
    vi.unstubAllGlobals()
  })

  it('resizing an image calls onSourceChange and onSave with updated width', async () => {
    const { onSourceChange, onSave } = renderMarkdownCell('<img src="/api/v1/attachments/abc123" alt="test" width="300">')

    const img = await screen.findByRole('img')
    expect(img).toBeTruthy()

    // Mock getBoundingClientRect so startWidth is predictable (jsdom returns 0)
    Object.defineProperty(img, 'getBoundingClientRect', {
      value: () => ({ width: 300 }),
      configurable: true,
    })

    const handle = document.querySelector('.img-resize-handle')!
    expect(handle).not.toBeNull()

    fireEvent.mouseDown(handle, { clientX: 100 })
    fireEvent.mouseMove(document, { clientX: 200 })
    fireEvent.mouseUp(document, { clientX: 200 })

    await waitFor(() => {
      expect(onSourceChange).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('width="400"')
      )
    })

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith(
        'cell-1',
        expect.stringContaining('width="400"')
      )
    })
  })
})
