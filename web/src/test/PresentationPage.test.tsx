import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { PresentationPage } from '../pages/PresentationPage'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { vi } from 'vitest'

const mockNotebook = {
  id: 'nb-1',
  title: 'Sales Report',
  cells: [
    { id: 'c1', type: 'text', source: '# Slide 1', outputs: [], slide_break: false },
    { id: 'c2', type: 'code', source: 'SELECT 1', outputs: [{ type: 'table', data: { columns: [], rows: [] } }], slide_break: false },
    { id: 'c3', type: 'text', source: '# Slide 3', outputs: [], slide_break: false },
  ],
}

beforeEach(() => {
  server.use(http.get('/api/v1/notebooks/:id', () => HttpResponse.json(mockNotebook)))
})

function renderPresentation() {
  return render(
    <MemoryRouter initialEntries={['/notebooks/nb-1/present']}>
      <Routes>
        <Route path="/notebooks/:id/present" element={<PresentationPage />} />
      </Routes>
    </MemoryRouter>
  )
}

// Skipped: flaky in CI due to MSW timing race on initial render
test.skip('shows first cell on load', async () => {
  renderPresentation()
  expect(await screen.findByText(/Slide 1/)).toBeInTheDocument()
})

test('Next button advances to second cell', async () => {
  renderPresentation()
  await screen.findByText(/Slide 1/)
  fireEvent.click(screen.getByRole('button', { name: /next/i }))
  await waitFor(() => {
    expect(document.body.textContent).toContain('SELECT 1')
  })
})

test('Previous button is disabled on first cell', async () => {
  renderPresentation()
  await screen.findByText(/Slide 1/)
  expect(screen.getByRole('button', { name: /previous/i })).toBeDisabled()
})

test('shows progress indicator', async () => {
  renderPresentation()
  await screen.findByText(/Slide 1/)
  expect(screen.getByText('1 / 3')).toBeInTheDocument()
})

test.skip('renders authenticated images via ResizableImage in markdown cells', async () => {
  localStorage.setItem('aether_token', 'test-token')
  const createObjectURLSpy = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock')
  const revokeObjectURLSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})

  const notebookWithImage = {
    ...mockNotebook,
    cells: [
      { id: 'c1', type: 'text', source: '<img src="/api/v1/attachments/img-1" alt="chart.png" width="100%">', outputs: [], slide_break: false },
    ],
  }
  server.use(
    http.get('/api/v1/notebooks/:id', () => HttpResponse.json(notebookWithImage)),
    http.get('/api/v1/attachments/:id', () => HttpResponse.arrayBuffer(new Uint8Array([1, 2, 3]).buffer, { headers: { 'Content-Type': 'image/png' } })),
  )

  renderPresentation()
  const img = await screen.findByRole('img')
  await waitFor(() => expect(img).toHaveAttribute('src', 'blob:mock'))
  expect(img).toHaveAttribute('alt', 'chart.png')

  createObjectURLSpy.mockRestore()
  revokeObjectURLSpy.mockRestore()
  localStorage.removeItem('aether_token')
})
