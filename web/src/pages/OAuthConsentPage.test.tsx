import { describe, test, expect, vi, beforeEach, afterEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { OAuthConsentPage } from './OAuthConsentPage'
import { renderWithProviders } from '../test/utils'

const AUTH_SEARCH = new URLSearchParams({
  client_id: 'client-1',
  redirect_uri: 'http://127.0.0.1:9876/callback',
  response_type: 'code',
  scope: 'mcp:query mcp:read',
  resource: 'http://org1.aether.test:8088/api/v1/mcp',
  state: 'st-1',
  code_challenge: 'abc123',
  code_challenge_method: 'S256',
}).toString()

interface FakeLocation {
  search: string
  href: string
  origin: string
}

// jsdom's window.location is unforgeable, so replace the global with a plain
// object: the page reads `search` and assigns `href` to leave for the client.
function stubLocation(search: string): FakeLocation {
  const fake: FakeLocation = {
    search,
    href: `http://localhost:3000/oauth/authorize${search}`,
    origin: 'http://localhost:3000',
  }
  vi.stubGlobal('location', fake)
  return fake
}

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('OAuthConsentPage', () => {
  test('shows an invalid-request error when required parameters are missing', async () => {
    stubLocation('')
    renderWithProviders(<OAuthConsentPage />)
    expect(await screen.findByText('Invalid authorization request.')).toBeInTheDocument()
  })

  test('asks the user to sign in when no token is stored', async () => {
    stubLocation(`?${AUTH_SEARCH}`)
    renderWithProviders(<OAuthConsentPage />)
    expect(await screen.findByText(/Sign in to Aether first/)).toBeInTheDocument()
  })

  test('renders the consent request and Allow posts the decision, then navigates', async () => {
    const location = stubLocation(`?${AUTH_SEARCH}`)
    localStorage.setItem('aether_token', 'test-token')

    let authHeader: string | null = null
    let decisionBody: Record<string, unknown> | null = null
    server.use(
      http.get('/api/v1/oauth/consent/info', ({ request }) => {
        const params = new URL(request.url).searchParams
        expect(params.get('client_id')).toBe('client-1')
        return HttpResponse.json({
          client_name: 'Claude Desktop',
          org_id: 'org-1',
          org_name: 'Acme',
          scopes: ['mcp:query', 'mcp:read'],
          scope_descriptions: {
            'mcp:query': 'Run read-only SQL queries against your connectors',
            'mcp:read': 'View notebooks, dashboards and other resources',
          },
        })
      }),
      http.post('/api/v1/oauth/consent/decision', async ({ request }) => {
        authHeader = request.headers.get('Authorization')
        decisionBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ redirect: 'http://127.0.0.1:9876/callback?code=abc&state=st-1' })
      }),
    )

    renderWithProviders(<OAuthConsentPage />)
    expect(
      await screen.findByRole('heading', { name: /Authorize Claude Desktop/ }),
    ).toBeInTheDocument()
    expect(screen.getByText('Acme')).toBeInTheDocument()
    expect(screen.getByText('Run read-only SQL queries against your connectors')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Allow' }))

    await waitFor(() =>
      expect(location.href).toBe('http://127.0.0.1:9876/callback?code=abc&state=st-1'),
    )
    expect(authHeader).toBe('Bearer test-token')
    expect(decisionBody).toMatchObject({
      client_id: 'client-1',
      redirect_uri: 'http://127.0.0.1:9876/callback',
      scope: 'mcp:query mcp:read',
      resource: 'http://org1.aether.test:8088/api/v1/mcp',
      state: 'st-1',
      code_challenge: 'abc123',
      code_challenge_method: 'S256',
      approve: true,
    })
  })

  test('Deny posts approve:false and follows the error redirect', async () => {
    const location = stubLocation(`?${AUTH_SEARCH}`)
    localStorage.setItem('aether_token', 'test-token')

    let decisionBody: Record<string, unknown> | null = null
    server.use(
      http.get('/api/v1/oauth/consent/info', () =>
        HttpResponse.json({
          client_name: 'Claude Desktop',
          org_id: 'org-1',
          org_name: 'Acme',
          scopes: ['mcp:query'],
          scope_descriptions: { 'mcp:query': 'Run read-only SQL queries against your connectors' },
        }),
      ),
      http.post('/api/v1/oauth/consent/decision', async ({ request }) => {
        decisionBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({
          redirect: 'http://127.0.0.1:9876/callback?error=access_denied&state=st-1',
        })
      }),
    )

    renderWithProviders(<OAuthConsentPage />)
    await screen.findByRole('heading', { name: /Authorize Claude Desktop/ })

    fireEvent.click(screen.getByRole('button', { name: 'Deny' }))

    await waitFor(() =>
      expect(location.href).toBe('http://127.0.0.1:9876/callback?error=access_denied&state=st-1'),
    )
    expect(decisionBody).toMatchObject({ approve: false })
  })
})
