import { describe, it, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { renderWithProviders, viewerUser } from './utils'
import { server } from './server'
import { StatsPage } from '../pages/StatsPage'

const ROWS = [
  {
    bucket_start: '2026-09-11T10:00:00Z',
    agent_id: 'a1',
    agent_name: 'Alpha',
    user_id: 'u1',
    user_name: 'Ann',
    user_email: 'ann@example.com',
    sessions_count: 1,
    messages_count: 2,
    tokens_input: 100,
    tokens_output: 50,
    tokens_direct: 7,
    tokens_subagent: 5,
    model_calls: 2,
    total_duration_ms: 3000,
    est_cost_usd: 0.0006,
  },
  {
    bucket_start: '2026-09-11T10:00:00Z',
    agent_id: 'a2',
    agent_name: 'Beta',
    user_id: 'u1',
    user_name: 'Ann',
    user_email: 'ann@example.com',
    sessions_count: 2,
    messages_count: 4,
    tokens_input: 200,
    tokens_output: 500,
    tokens_direct: 0,
    tokens_subagent: 0,
    model_calls: 3,
    total_duration_ms: 1000,
    est_cost_usd: 0.001,
  },
]

let getCalls = 0
let postCalls = 0

beforeEach(() => {
  getCalls = 0
  postCalls = 0
  localStorage.clear()
  server.use(
    http.get('/api/v1/agents/stats', () => {
      getCalls++
      return HttpResponse.json(ROWS)
    }),
    http.post('/api/v1/agents/stats/rollup', () => {
      postCalls++
      return HttpResponse.json({ rolled_up: { bucket_from: '2026-09-11T09:00:00Z', bucket_to: '2026-09-11T10:00:00Z', rows: 2 } })
    }),
  )
})

describe('StatsPage', () => {
  it('renders KPIs, chart, and agent table from stats rows', async () => {
    renderWithProviders(<StatsPage />)
    await waitFor(() => expect(screen.getAllByText('Alpha').length).toBeGreaterThan(0))
    // KPIs: 3 sessions, 6 messages, 550 out → tokens, cost 0.0016
    expect(screen.getByText('$0.0016')).toBeDefined()
    expect(screen.getByRole('option', { name: 'Beta' })).toBeDefined()
    expect(screen.getAllByText('Beta').length).toBeGreaterThan(1)
    // Agent filter derived from rows
    expect(screen.getByRole('option', { name: 'Alpha' })).toBeDefined()
  })

  it('roll up now posts and refreshes the query', async () => {
    renderWithProviders(<StatsPage />)
    await waitFor(() => expect(screen.getAllByText('Alpha').length).toBeGreaterThan(0))
    const before = getCalls
    fireEvent.click(screen.getByText(/Roll up now/))
    await waitFor(() => expect(postCalls).toBe(1))
    await waitFor(() => expect(getCalls).toBeGreaterThan(before))
    await waitFor(() => expect(screen.getByText(/Rolled up 2 bucket row/)).toBeDefined())
  })

  it('row click drills into the agent filter', async () => {
    renderWithProviders(<StatsPage />)
    await waitFor(() => expect(screen.getAllByText('Alpha').length).toBeGreaterThan(0))
    const betaCell = screen.getAllByText('Beta').find((el) => el.tagName === 'TD')
    fireEvent.click(betaCell!)
    await waitFor(() => {
      const sel = screen.getByLabelText('Filter by agent') as HTMLSelectElement
      expect(sel.value).toBe('a2')
    })
  })

  it('denies non-admins without fetching', async () => {
    renderWithProviders(<StatsPage />, { user: viewerUser() })
    await waitFor(() => expect(screen.getByText('Admin access required')).toBeDefined())
  })
})
