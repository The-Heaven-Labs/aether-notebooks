import { screen, fireEvent, waitFor } from '@testing-library/react'
import { OrgSettingsPage } from '../pages/OrgSettingsPage'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { renderWithProviders, viewerUser } from './utils'

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

// ─── Branding ───────────────────────────────────────────────────────────────

const TEAL_PALETTE = ['#14b8a6', '#06b6d4', '#0ea5e9', '#6366f1', '#8b5cf6', '#10b981', '#f59e0b', '#f43f5e']
const AMBER_PALETTE = ['#f59e0b', '#fb923c', '#f43f5e', '#8b5cf6', '#0ea5e9', '#84cc16', '#e879f9', '#14b8a6']

const BRANDED_SETTINGS = {
  audit_retention_days: 30,
  default_max_rows: 1000,
  branding: {
    app_name: 'Heaven Labs',
    accent_color: '#6366f1',
    logo_attachment_id: 'att-1',
    default_chart_palette: TEAL_PALETTE,
  },
}

function captureSettingsPuts(bodies: Array<Record<string, unknown>>) {
  return http.put('/api/v1/org/settings', async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>
    bodies.push(body)
    return HttpResponse.json(body)
  })
}

/** Renders the page and waits for the branding values to arrive from the GET. */
async function renderBranding(settings: Record<string, unknown>) {
  server.use(http.get('/api/v1/org/settings', () => HttpResponse.json(settings)))
  renderWithProviders(<OrgSettingsPage />, { initialPath: '/settings' })
  const appName = await screen.findByLabelText('App name')
  const branding = settings.branding as { app_name?: string } | undefined
  if (branding?.app_name) await waitFor(() => expect(appName).toHaveValue(branding.app_name as string))
  return appName
}

test('loads the current branding values from org settings', async () => {
  await renderBranding(BRANDED_SETTINGS)

  expect(screen.getByLabelText('App name')).toHaveValue('Heaven Labs')
  expect(screen.getByLabelText('Accent color')).toHaveValue('#6366f1')
  expect(screen.getByLabelText('Chart palette')).toHaveValue('teal')
  expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/att-1')
})

test('saves the full settings object with the updated branding', async () => {
  const bodies: Array<Record<string, unknown>> = []
  const appName = await renderBranding(BRANDED_SETTINGS)
  server.use(captureSettingsPuts(bodies))

  fireEvent.change(appName, { target: { value: 'Acme Data' } })
  fireEvent.change(screen.getByLabelText('Accent color'), { target: { value: '#ff0000' } })
  fireEvent.change(screen.getByLabelText('Chart palette'), { target: { value: 'amber' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save branding' }))

  await waitFor(() => expect(bodies.length).toBe(1))
  // The save carries the full settings object — the fetched fields (audit
  // retention, max rows) alongside the new branding. The handler top-level-
  // merges the body and replaces only `branding` wholesale; the full payload is
  // the plan's invariant, so the assertion pins it.
  expect(bodies[0]).toEqual({
    audit_retention_days: 30,
    default_max_rows: 1000,
    branding: {
      app_name: 'Acme Data',
      accent_color: '#ff0000',
      logo_attachment_id: 'att-1',
      default_chart_palette: AMBER_PALETTE,
    },
  })
})

test('uploads a logo, previews it, and saves its attachment id', async () => {
  const bodies: Array<Record<string, unknown>> = []
  let uploads = 0
  await renderBranding(BRANDED_SETTINGS)
  server.use(
    http.post('/api/v1/org/attachments', () => {
      uploads++
      return HttpResponse.json({ id: 'att-9', filename: 'logo.png', mime_type: 'image/png', size: 3 }, { status: 201 })
    }),
    captureSettingsPuts(bodies),
  )

  fireEvent.change(screen.getByLabelText('Logo file'), {
    target: { files: [new File(['png'], 'logo.png', { type: 'image/png' })] },
  })

  await waitFor(() => expect(uploads).toBe(1))
  await waitFor(() =>
    expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/att-9'),
  )

  fireEvent.click(screen.getByRole('button', { name: 'Save branding' }))
  await waitFor(() => expect(bodies.length).toBe(1))
  expect(bodies[0]).toEqual({
    audit_retention_days: 30,
    default_max_rows: 1000,
    branding: {
      app_name: 'Heaven Labs',
      accent_color: '#6366f1',
      logo_attachment_id: 'att-9',
      default_chart_palette: TEAL_PALETTE,
    },
  })
})

test('clearing the logo removes it from the saved branding', async () => {
  const bodies: Array<Record<string, unknown>> = []
  await renderBranding(BRANDED_SETTINGS)
  server.use(captureSettingsPuts(bodies))

  fireEvent.click(await screen.findByRole('button', { name: 'Clear logo' }))
  expect(screen.queryByAltText('Logo preview')).not.toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Save branding' }))
  await waitFor(() => expect(bodies.length).toBe(1))
  expect((bodies[0].branding as Record<string, unknown>)).toEqual({
    app_name: 'Heaven Labs',
    accent_color: '#6366f1',
    default_chart_palette: TEAL_PALETTE,
  })
})

test('clearing every branding value sends an empty branding object', async () => {
  const bodies: Array<Record<string, unknown>> = []
  const appName = await renderBranding(BRANDED_SETTINGS)
  server.use(captureSettingsPuts(bodies))

  fireEvent.change(appName, { target: { value: '' } })
  fireEvent.change(screen.getByLabelText('Accent color'), { target: { value: '' } })
  fireEvent.change(screen.getByLabelText('Chart palette'), { target: { value: 'default' } })
  fireEvent.click(await screen.findByRole('button', { name: 'Clear logo' }))
  fireEvent.click(screen.getByRole('button', { name: 'Save branding' }))

  await waitFor(() => expect(bodies.length).toBe(1))
  // `branding: {}` is the server's documented clear path (an explicit null is
  // dropped by omitempty), so the key must always be present.
  expect(bodies[0]).toEqual({
    audit_retention_days: 30,
    default_max_rows: 1000,
    branding: {},
  })
})

test('saves a custom chart palette', async () => {
  const bodies: Array<Record<string, unknown>> = []
  await renderBranding(BRANDED_SETTINGS)
  server.use(captureSettingsPuts(bodies))

  fireEvent.change(screen.getByLabelText('Chart palette'), { target: { value: 'custom' } })
  fireEvent.change(screen.getByLabelText('Custom chart colors'), { target: { value: '#111111, #222222' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save branding' }))

  await waitFor(() => expect(bodies.length).toBe(1))
  const branding = bodies[0].branding as Record<string, unknown>
  expect(branding.default_chart_palette).toEqual(['#111111', '#222222'])
})

test('rejects an invalid accent color before saving', async () => {
  const bodies: Array<Record<string, unknown>> = []
  await renderBranding(BRANDED_SETTINGS)
  server.use(captureSettingsPuts(bodies))

  fireEvent.change(screen.getByLabelText('Accent color'), { target: { value: 'red' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save branding' }))

  expect(await screen.findByRole('alert')).toHaveTextContent('hex color')
  expect(bodies.length).toBe(0)
})

test('non-admins do not see the branding section', async () => {
  server.use(http.get('/api/v1/org/settings', () => HttpResponse.json(BRANDED_SETTINGS)))
  renderWithProviders(<OrgSettingsPage />, { initialPath: '/settings', user: viewerUser() })

  expect(await screen.findByText('Public Sharing')).toBeInTheDocument()
  expect(screen.queryByText('Branding')).not.toBeInTheDocument()
  expect(screen.queryByLabelText('App name')).not.toBeInTheDocument()
})

test('keeps Save disabled until the settings GET resolves', async () => {
  let release: (() => void) | undefined
  server.use(http.get('/api/v1/org/settings', async () => {
    await new Promise<void>(resolve => { release = resolve })
    return HttpResponse.json(BRANDED_SETTINGS)
  }))
  renderWithProviders(<OrgSettingsPage />, { initialPath: '/settings' })

  const save = await screen.findByRole('button', { name: 'Save branding' })
  expect(save).toBeDisabled()

  await waitFor(() => expect(release).toBeDefined())
  release?.()
  await waitFor(() => expect(save).not.toBeDisabled())
})

test('shows a load error and keeps Save disabled when the settings GET fails', async () => {
  server.use(http.get('/api/v1/org/settings', () =>
    HttpResponse.json({ error: 'settings unavailable' }, { status: 500 }),
  ))
  renderWithProviders(<OrgSettingsPage />, { initialPath: '/settings' })

  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('Failed to load org settings')
  expect(alert).toHaveTextContent('settings unavailable')
  expect(screen.getByRole('button', { name: 'Save branding' })).toBeDisabled()
})

test('matches a stored palette against presets case-insensitively', async () => {
  server.use(http.get('/api/v1/org/settings', () => HttpResponse.json({
    audit_retention_days: 7,
    branding: { default_chart_palette: TEAL_PALETTE.map(color => color.toUpperCase()) },
  })))
  renderWithProviders(<OrgSettingsPage />, { initialPath: '/settings' })

  await waitFor(() => expect(screen.getByLabelText('Chart palette')).toHaveValue('teal'))
})

test('a failed logo upload surfaces the error and keeps the current logo', async () => {
  await renderBranding(BRANDED_SETTINGS)
  server.use(http.post('/api/v1/org/attachments', () =>
    HttpResponse.json({ error: 'storage error' }, { status: 500 }),
  ))

  fireEvent.change(screen.getByLabelText('Logo file'), {
    target: { files: [new File(['png'], 'logo.png', { type: 'image/png' })] },
  })

  expect(await screen.findByRole('alert')).toHaveTextContent('storage error')
  expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/att-1')
})

test('rejects a custom palette with more than 12 colors', async () => {
  const bodies: Array<Record<string, unknown>> = []
  await renderBranding(BRANDED_SETTINGS)
  server.use(captureSettingsPuts(bodies))

  fireEvent.change(screen.getByLabelText('Chart palette'), { target: { value: 'custom' } })
  const tooMany = Array.from({ length: 13 }, (_, i) => `#${i.toString(16).padStart(6, '0')}`).join(', ')
  fireEvent.change(screen.getByLabelText('Custom chart colors'), { target: { value: tooMany } })
  fireEvent.click(screen.getByRole('button', { name: 'Save branding' }))

  expect(await screen.findByRole('alert')).toHaveTextContent('at most 12 colors')
  expect(bodies.length).toBe(0)
})

test('counts the app name in runes, not UTF-16 units', async () => {
  const bodies: Array<Record<string, unknown>> = []
  const appName = await renderBranding(BRANDED_SETTINGS)
  server.use(captureSettingsPuts(bodies))

  fireEvent.change(appName, { target: { value: 'é'.repeat(61) } })
  fireEvent.click(screen.getByRole('button', { name: 'Save branding' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('at most 60 characters')
  expect(bodies.length).toBe(0)

  // 60 code points but 120 UTF-16 units: a naive `.length` check would reject it.
  fireEvent.change(appName, { target: { value: 'é'.repeat(60) } })
  fireEvent.click(screen.getByRole('button', { name: 'Save branding' }))
  await waitFor(() => expect(bodies.length).toBe(1))
  const branding = bodies[0].branding as Record<string, unknown>
  expect(branding.app_name).toBe('é'.repeat(60))
})

test('keeps Save disabled while a logo upload is in flight', async () => {
  let started = false
  let release: (() => void) | undefined
  await renderBranding(BRANDED_SETTINGS)
  server.use(http.post('/api/v1/org/attachments', async () => {
    started = true
    await new Promise<void>(resolve => { release = resolve })
    return HttpResponse.json({ id: 'att-9', filename: 'logo.png', mime_type: 'image/png', size: 3 }, { status: 201 })
  }))

  fireEvent.change(screen.getByLabelText('Logo file'), {
    target: { files: [new File(['png'], 'logo.png', { type: 'image/png' })] },
  })

  await waitFor(() => expect(started).toBe(true))
  expect(screen.getByRole('button', { name: 'Save branding' })).toBeDisabled()
  release?.()
  await waitFor(() => expect(screen.getByRole('button', { name: 'Save branding' })).not.toBeDisabled())
})
