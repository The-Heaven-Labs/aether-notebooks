import { render, screen, fireEvent } from '@testing-library/react'
import { PresentationPage } from '../pages/PresentationPage'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

const mockNotebook = {
  id: 'nb-1',
  title: 'Sales Report',
  cells: [
    { id: 'c1', type: 'text', source: '# Slide 1', outputs: [], slide_break: false },
    { id: 'c2', type: 'code', source: 'SELECT 1', outputs: [{ type: 'table', data: { columns: [], rows: [] } }], slide_break: true },
    { id: 'c3', type: 'text', source: '# Slide 3', outputs: [], slide_break: true },
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

test('shows first cell on load', async () => {
  renderPresentation()
  expect(await screen.findByText(/Slide 1/)).toBeInTheDocument()
})

test('Next button advances to second cell', async () => {
  renderPresentation()
  await screen.findByText(/Slide 1/)
  fireEvent.click(screen.getByRole('button', { name: /next/i }))
  expect(screen.queryByText('SELECT 1')).not.toBeInTheDocument()
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
