import { screen, fireEvent, waitFor } from '@testing-library/react'
import { AdminPage } from '../pages/AdminPage'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { renderWithProviders } from './utils'

beforeEach(() => {
  server.use(
    http.get('/api/v1/admin/orgs', () => HttpResponse.json({
      orgs: [{ id: 'o1', name: 'Acme Corp', slug: 'acme', member_count: 5, created_at: '2024-01-01' }]
    })),
    http.get('/api/v1/admin/users', () => HttpResponse.json({
      users: [{ id: 'u1', email: 'admin@acme.com', name: 'Admin', is_platform_admin: true, orgs: ['Acme Corp'] }]
    }))
  )
})

function renderAdmin() {
  localStorage.setItem('aether_is_platform_admin', 'true')
  return renderWithProviders(<AdminPage />, { initialPath: '/admin' })
}

test('shows orgs list', async () => {
  renderAdmin()
  expect(await screen.findByText('Acme Corp')).toBeInTheDocument()
  expect(screen.getByText('5')).toBeInTheDocument()
})

test('shows users list', async () => {
  renderAdmin()
  const usersTab = await screen.findByRole('tab', { name: /users/i })
  usersTab.click()
  expect(await screen.findByText('admin@acme.com')).toBeInTheDocument()
})

const PROVIDER = {
  id: 'p1', scope: 'platform', scopes: ['openid', 'groups'], groups_claim: 'groups',
  group_prefix: 'aether-', auto_sync_groups: true, get_user_info: true,
  sync_empty_groups: false, strip_group_prefix: false, debug_claims: true,
  name: 'Keycloak (Dev)', provider_type: 'oidc', client_id: 'aether-dev',
  discovery_url: 'http://localhost:5557/realms/aether-dev', allowed_domains: ['aether-dev.test'],
  enabled: true, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
}

async function openSSOTab() {
  renderAdmin()
  const ssoTab = await screen.findByRole('tab', { name: /sso/i })
  ssoTab.click()
}

test('shows the captured IDP payload for a provider with Debug Claims enabled', async () => {
  server.use(
    http.get('/api/v1/admin/sso/providers', () => HttpResponse.json({ providers: [PROVIDER] })),
    http.get('/api/v1/admin/sso/providers/p1/debug-claims', () =>
      HttpResponse.json({
        captured_at: '2026-09-22T10:00:00Z',
        provider_id: 'p1', subject: 'sub-1', email: 'alice@aether-dev.test', name: 'Alice',
        granted_scopes: ['openid', 'groups'],
        id_token_claims: { email: 'alice@aether-dev.test', groups: ['aether-analysts'] },
        user_info_claims: { groups: ['aether-analysts'] },
        groups_claim: 'groups', groups_claim_value: ['aether-analysts'],
        parsed_groups: ['aether-analysts', 'other-team'],
      }),
    ),
  )
  await openSSOTab()
  fireEvent.click(await screen.findByRole('button', { name: 'Debug Claims' }))

  expect(await screen.findByText(/^Captured /)).toBeInTheDocument()
  expect(screen.getAllByText(/alice@aether-dev.test/).length).toBeGreaterThan(0)
  expect(screen.getByText('openid, groups')).toBeInTheDocument()
  expect(screen.getByText('aether-analysts, other-team')).toBeInTheDocument()
  // The derived strip shows the prefix filter dropping non-aether-* groups.
  const afterFilterRow = screen.getByText('After prefix filter').parentElement
  expect(afterFilterRow?.textContent).toBe('After prefix filteraether-analysts')
})

test('shows the Debug Claims empty state before a capture exists', async () => {
  server.use(
    http.get('/api/v1/admin/sso/providers', () => HttpResponse.json({ providers: [PROVIDER] })),
    http.get('/api/v1/admin/sso/providers/p1/debug-claims', () =>
      HttpResponse.json({ error: 'no capture yet' }, { status: 404 }),
    ),
  )
  await openSSOTab()
  fireEvent.click(await screen.findByRole('button', { name: 'Debug Claims' }))

  expect(await screen.findByText(/No capture yet/)).toBeInTheDocument()
})

test('persists the Debug Claims toggle through the provider form', async () => {
  let putBody: Record<string, unknown> | null = null
  server.use(
    http.get('/api/v1/admin/sso/providers', () => HttpResponse.json({ providers: [PROVIDER] })),
    http.put('/api/v1/admin/sso/providers/p1', async ({ request }) => {
      putBody = await request.json() as Record<string, unknown>
      return HttpResponse.json(PROVIDER)
    }),
  )
  await openSSOTab()
  fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))

  const checkbox = await screen.findByLabelText('Debug Claims')
  expect(checkbox).toBeChecked()
  fireEvent.click(checkbox)
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

  await waitFor(() => expect(putBody?.debug_claims).toBe(false))
})
