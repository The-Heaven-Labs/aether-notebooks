import { screen, fireEvent, waitFor } from '@testing-library/react'
import { OrgSettingsPage } from '../pages/OrgSettingsPage'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { renderWithProviders } from './utils'

const PROVIDER = {
  id: 'p1', scope: 'org', scopes: ['openid', 'groups'], groups_claim: 'groups',
  group_prefix: 'aether-', auto_sync_groups: true, get_user_info: true,
  sync_empty_groups: false, strip_group_prefix: false, debug_claims: true,
  name: 'Org Keycloak', provider_type: 'oidc', client_id: 'aether-org',
  discovery_url: 'http://localhost:5557/realms/aether-org', allowed_domains: ['aether-org.test'],
  enabled: true, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
}

beforeEach(() => {
  server.use(
    http.get('/api/v1/motd', () => HttpResponse.json([])),
    http.get('/api/v1/admin/motd', () => HttpResponse.json([])),
    http.get('/api/v1/sso/platform-providers', () => HttpResponse.json({ providers: [] })),
    http.get('/api/v1/sso/providers', () => HttpResponse.json({ providers: [PROVIDER] })),
    http.get('/api/v1/sso/settings', () => HttpResponse.json({ sso_password_login: true })),
    http.get('/api/v1/org/output-limits', () => HttpResponse.json({
      cell_output_max_bytes: 10485760, cell_output_max_bytes_resolved: 10485760,
      notebook_inline_outputs_max_bytes: 33554432, notebook_inline_outputs_max_bytes_resolved: 33554432,
      platform_max_bytes: 0,
    })),
    http.get('/api/v1/org/sharing', () => HttpResponse.json({ public_sharing_enabled: true })),
    http.get('/api/v1/org/invitations', () => HttpResponse.json({ invitations_enabled: true })),
    http.get('/api/v1/org/registration', () => HttpResponse.json({ registration_enabled: true })),
    http.get('/api/v1/org/data-export', () => HttpResponse.json({ data_export_enabled: true })),
    http.get('/api/v1/org/settings', () => HttpResponse.json({ audit_retention_days: 7 })),
  )
})

test('shows the captured IDP payload for an org provider with Debug Claims enabled', async () => {
  server.use(
    http.get('/api/v1/sso/providers/p1/debug-claims', () =>
      HttpResponse.json({
        captured_at: '2026-09-22T10:00:00Z',
        provider_id: 'p1', subject: 'sub-1', email: 'alice@aether-org.test', name: 'Alice',
        granted_scopes: ['openid', 'groups'],
        id_token_claims: { email: 'alice@aether-org.test', groups: ['aether-analysts'] },
        user_info_claims: { groups: ['aether-analysts'] },
        groups_claim: 'groups', groups_claim_value: ['aether-analysts'],
        parsed_groups: ['aether-analysts', 'other-team'],
      }),
    ),
  )
  renderWithProviders(<OrgSettingsPage />, { initialPath: '/settings' })

  fireEvent.click(await screen.findByRole('button', { name: 'Debug Claims' }))

  expect(await screen.findByText(/^Captured /)).toBeInTheDocument()
  expect(screen.getAllByText(/alice@aether-org.test/).length).toBeGreaterThan(0)
  expect(screen.getByText('openid, groups')).toBeInTheDocument()
  const afterFilterRow = screen.getByText('After prefix filter').parentElement
  expect(afterFilterRow?.textContent).toBe('After prefix filteraether-analysts')
})

test('shows the Debug Claims empty state before a capture exists', async () => {
  server.use(
    http.get('/api/v1/sso/providers/p1/debug-claims', () =>
      HttpResponse.json({ error: 'no capture yet' }, { status: 404 }),
    ),
  )
  renderWithProviders(<OrgSettingsPage />, { initialPath: '/settings' })

  fireEvent.click(await screen.findByRole('button', { name: 'Debug Claims' }))

  expect(await screen.findByText(/No capture yet/)).toBeInTheDocument()
})

test('persists the Debug Claims toggle through the provider form', async () => {
  let putBody: Record<string, unknown> | null = null
  server.use(
    http.put('/api/v1/sso/providers/p1', async ({ request }) => {
      putBody = await request.json() as Record<string, unknown>
      return HttpResponse.json(PROVIDER)
    }),
  )
  renderWithProviders(<OrgSettingsPage />, { initialPath: '/settings' })

  fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))

  const checkbox = await screen.findByLabelText('Debug Claims')
  expect(checkbox).toBeChecked()
  fireEvent.click(checkbox)
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

  await waitFor(() => expect(putBody?.debug_claims).toBe(false))
})

test('hides the Debug Claims button for providers without the flag', async () => {
  server.use(
    http.get('/api/v1/sso/providers', () => HttpResponse.json({
      providers: [{ ...PROVIDER, debug_claims: false }],
    })),
  )
  renderWithProviders(<OrgSettingsPage />, { initialPath: '/settings' })

  expect(await screen.findByText('Org Keycloak')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Debug Claims' })).not.toBeInTheDocument()
})
