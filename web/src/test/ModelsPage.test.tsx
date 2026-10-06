import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { ModelsPage } from '../pages/ModelsPage'
import { renderWithProviders } from './utils'

vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

const mockModels = [
  {
    id: 'm-1',
    name: 'GPT-4o',
    provider: 'openai',
    base_url: 'https://api.openai.com/v1',
    model: 'gpt-4o',
    context_window: 128000,
    default_params: { compaction_threshold: 70 },
    price_per_input_token: 2.5,
    price_per_output_token: 10,
    price_per_cache_read_token: 1.25,
    created_at: '2026-01-01T00:00:00Z',
  },
]

beforeEach(() => {
  server.use(http.get('/api/v1/model-configs', () => HttpResponse.json(mockModels)))
  vi.clearAllMocks()
})

describe('ModelsPage', () => {
  test('shows the model list with icon row actions', async () => {
    renderWithProviders(<ModelsPage />)
    expect(await screen.findByText('GPT-4o')).toBeInTheDocument()
    expect(screen.getByLabelText('Test model config')).toBeInTheDocument()
    expect(screen.getByLabelText('Edit model config')).toBeInTheDocument()
    expect(screen.getByLabelText('Permissions')).toBeInTheDocument()
    expect(screen.getByLabelText('Delete model config')).toBeInTheDocument()
  })

  test('creates a model config through the modal', async () => {
    let postBody: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/model-configs', async ({ request }) => {
        postBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ id: 'm-new' }, { status: 201 })
      }),
    )
    renderWithProviders(<ModelsPage />)
    await screen.findByText('GPT-4o')

    const newButton = screen.getByText('+ New Model')
    newButton.focus()
    fireEvent.click(newButton)

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('New Model')
    expect(screen.getByLabelText('Name')).toHaveFocus()

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Claude' } })
    fireEvent.change(screen.getByLabelText('Provider'), { target: { value: 'anthropic' } })
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'claude-sonnet-4-20250514' } })
    fireEvent.click(within(dialog).getByText('Create'))

    await waitFor(() => expect(postBody?.name).toBe('Claude'))
    expect(postBody).toMatchObject({ provider: 'anthropic' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  test('edits a model config through the modal and keeps a blank API key', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/model-configs/m-1', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ id: 'm-1' })
      }),
    )
    renderWithProviders(<ModelsPage />)
    await screen.findByText('GPT-4o')

    fireEvent.click(screen.getByLabelText('Edit model config'))
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('Edit "GPT-4o"')

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'GPT-4o mini' } })
    fireEvent.click(within(dialog).getByText('Save'))

    await waitFor(() => expect(putBody?.name).toBe('GPT-4o mini'))
    // A blank API key keeps the stored secret (the field is omitted).
    expect(putBody).not.toHaveProperty('api_key')
  })

  test('tests a model config from the row action', async () => {
    server.use(
      http.post('/api/v1/model-configs/m-1/test', () =>
        HttpResponse.json({ status: 'ok', response: 'pong', model: 'gpt-4o' }),
      ),
    )
    renderWithProviders(<ModelsPage />)
    await screen.findByText('GPT-4o')

    fireEvent.click(screen.getByLabelText('Test model config'))
    expect(await screen.findByText('Connected')).toBeInTheDocument()
  })

  test('deletes a model config after confirmation', async () => {
    let deleted = false
    server.use(
      http.delete('/api/v1/model-configs/m-1', () => {
        deleted = true
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithProviders(<ModelsPage />)
    await screen.findByText('GPT-4o')

    fireEvent.click(screen.getByLabelText('Delete model config'))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByText('Delete'))

    await waitFor(() => expect(deleted).toBe(true))
  })
})
