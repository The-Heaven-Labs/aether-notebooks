import { describe, test, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { AppSettingsPanel } from './AppSettingsPanel'
import type { Dashboard, OrgBranding, Widget } from '../types'

// The panel clears a deleted tab's placements through the shared document when
// one is bound; these tests stub the runtime so no provider is ever created.
vi.mock('./dashboardCollabRuntime', () => ({
  peekDashboardCollab: vi.fn(),
  setWidgetTab: vi.fn(),
}))
import { peekDashboardCollab, setWidgetTab } from './dashboardCollabRuntime'

type PanelDashboard = Dashboard & { org_branding?: OrgBranding }

function dashboardWith(settings: Dashboard['settings'], orgBranding?: OrgBranding): PanelDashboard {
  return {
    id: 'd1',
    org_id: 'org-1',
    title: 'Test Dashboard',
    settings,
    org_branding: orgBranding,
    created_by: 'u1',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function widget(overrides: Partial<Widget> = {}): Widget {
  return {
    id: 'w1',
    dashboard_id: 'd1',
    connector_id: 'conn-1',
    query: 'SELECT 1',
    language: 'sql',
    type: 'table',
    layout: { row: 0, col: 0, width: 6, height: 6 },
    config: {},
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

beforeEach(() => {
  vi.mocked(peekDashboardCollab).mockReset()
  vi.mocked(setWidgetTab).mockClear()
})

function capturePuts() {
  const bodies: Array<Record<string, unknown>> = []
  server.use(
    http.put('/api/v1/dashboards/d1', async ({ request }) => {
      bodies.push((await request.json()) as Record<string, unknown>)
      return HttpResponse.json({})
    }),
  )
  return bodies
}

function renderPanel(
  dashboard: PanelDashboard,
  onSaved = vi.fn(),
  onClose = vi.fn(),
  options: { widgets?: Widget[]; initialSection?: 'app' | 'tabs' } = {},
) {
  render(
    <AppSettingsPanel
      dashboardId="d1"
      dashboard={dashboard}
      widgets={options.widgets}
      initialSection={options.initialSection}
      onClose={onClose}
      onSaved={onSaved}
    />,
  )
  return { onSaved, onClose }
}

/** The `app` block of the first captured PUT body. */
function savedApp(bodies: Array<Record<string, unknown>>): Record<string, unknown> {
  return (bodies[0].settings as Record<string, unknown>).app as Record<string, unknown>
}

describe('AppSettingsPanel', () => {
  test('renders current app settings', () => {
    renderPanel(dashboardWith({
      app: {
        title: 'Q3 Revenue',
        subtitle: 'Sales ops · updated hourly',
        width: 'full',
        show_header: false,
        show_refresh: true,
        show_last_updated: false,
        theme: {
          accent: '#ff0000',
          radius: 8,
          font_family: 'jetbrains-mono',
          color_scheme: 'dark',
          chart_palette: ['#111111', '#222222'],
        },
      },
    }))
    expect((screen.getByLabelText('App title') as HTMLInputElement).value).toBe('Q3 Revenue')
    expect((screen.getByLabelText('Subtitle') as HTMLInputElement).value).toBe('Sales ops · updated hourly')
    expect((screen.getByLabelText('Width') as HTMLSelectElement).value).toBe('full')
    expect((screen.getByLabelText('Show header') as HTMLInputElement).checked).toBe(false)
    expect((screen.getByLabelText('Show refresh') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('Show last updated') as HTMLInputElement).checked).toBe(false)
    expect(screen.getByRole('heading', { name: 'Theme', level: 3 })).toBeInTheDocument()
    expect((screen.getByLabelText('Accent') as HTMLInputElement).value).toBe('#ff0000')
    expect((screen.getByLabelText('Corner radius') as HTMLInputElement).value).toBe('8')
    expect((screen.getByLabelText('Font family') as HTMLSelectElement).value).toBe('jetbrains-mono')
    expect((screen.getByLabelText('Color scheme') as HTMLSelectElement).value).toBe('dark')
    expect((screen.getByLabelText('Chart palette') as HTMLSelectElement).value).toBe('custom')
    expect((screen.getByLabelText('Custom chart colors') as HTMLInputElement).value).toBe('#111111, #222222')
    expect(screen.getByRole('heading', { name: 'Logo', level: 3 })).toBeInTheDocument()
  })

  test('unset toggles default to shown and the width to fixed', () => {
    renderPanel(dashboardWith({}))
    expect((screen.getByLabelText('Show header') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('Show refresh') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('Show last updated') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('Width') as HTMLSelectElement).value).toBe('fixed')
    expect((screen.getByLabelText('Accent') as HTMLInputElement).value).toBe('')
    expect((screen.getByLabelText('Font family') as HTMLSelectElement).value).toBe('')
    expect((screen.getByLabelText('Color scheme') as HTMLSelectElement).value).toBe('auto')
    expect((screen.getByLabelText('Chart palette') as HTMLSelectElement).value).toBe('default')
  })

  test('app title placeholder falls back to the dashboard title', () => {
    renderPanel(dashboardWith({}))
    expect((screen.getByLabelText('App title') as HTMLInputElement).placeholder).toBe('Test Dashboard')
  })

  test('saves explicit booleans and preserves sibling settings', async () => {
    const bodies = capturePuts()
    const { onSaved } = renderPanel(dashboardWith({
      public_live: true,
      variables: [{ name: 'region', type: 'text' }],
      app: { logo_attachment_id: 'att-1' },
    }))

    fireEvent.change(screen.getByLabelText('App title'), { target: { value: 'Q3 Revenue' } })
    fireEvent.change(screen.getByLabelText('Subtitle'), { target: { value: 'Sales ops' } })
    fireEvent.change(screen.getByLabelText('Width'), { target: { value: 'full' } })
    fireEvent.click(screen.getByLabelText('Show refresh'))
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    expect(bodies[0].settings).toEqual({
      public_live: true,
      variables: [{ name: 'region', type: 'text' }],
      app: {
        logo_attachment_id: 'att-1',
        title: 'Q3 Revenue',
        subtitle: 'Sales ops',
        width: 'full',
        show_header: true,
        show_refresh: false,
        show_last_updated: true,
      },
      tabs: [],
    })
    expect(onSaved).toHaveBeenCalledTimes(1)
  })

  test('clearing title and subtitle omits them from the payload', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({
      app: { title: 'Old title', subtitle: 'Old subtitle', logo_attachment_id: 'att-1' },
    }))

    fireEvent.change(screen.getByLabelText('App title'), { target: { value: '  ' } })
    fireEvent.change(screen.getByLabelText('Subtitle'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    const app = savedApp(bodies)
    expect(app).toEqual({
      logo_attachment_id: 'att-1',
      width: 'fixed',
      show_header: true,
      show_refresh: true,
      show_last_updated: true,
    })
    expect('title' in app).toBe(false)
    expect('subtitle' in app).toBe(false)
  })

  test('shows the server error when saving fails', async () => {
    server.use(
      http.put('/api/v1/dashboards/d1', () =>
        HttpResponse.json({ error: 'app title must be at most 120 characters' }, { status: 400 }),
      ),
    )
    renderPanel(dashboardWith({}))
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('app title must be at most 120 characters')
  })

  test('closes on Escape and on backdrop click', () => {
    const { onClose } = renderPanel(dashboardWith({}))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByTestId('app-settings-backdrop'))
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  test('changing the accent color saves it into the theme', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ app: { theme: { radius: 8 } } }))

    fireEvent.change(screen.getByLabelText('Accent'), { target: { value: '#ff0000' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    expect(savedApp(bodies).theme).toEqual({ radius: 8, accent: '#ff0000' })
  })

  test('color scheme and font family round-trip into the save payload', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ app: {} }))

    fireEvent.change(screen.getByLabelText('Color scheme'), { target: { value: 'dark' } })
    fireEvent.change(screen.getByLabelText('Font family'), { target: { value: 'jetbrains-mono' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    expect(savedApp(bodies).theme).toEqual({ color_scheme: 'dark', font_family: 'jetbrains-mono' })
  })

  test('choosing a preset chart palette saves the preset hexes', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ app: {} }))

    fireEvent.change(screen.getByLabelText('Chart palette'), { target: { value: 'aether' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    const palette = (savedApp(bodies).theme as Record<string, unknown>).chart_palette as string[]
    expect(palette).toHaveLength(8)
    expect(palette[0]).toBe('#6366f1')
    expect(palette[7]).toBe('#84cc16')
  })

  test('a custom chart palette saves the parsed hex list', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ app: {} }))

    fireEvent.change(screen.getByLabelText('Chart palette'), { target: { value: 'custom' } })
    fireEvent.change(screen.getByLabelText('Custom chart colors'), { target: { value: '#111111, #222222' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    expect((savedApp(bodies).theme as Record<string, unknown>).chart_palette).toEqual(['#111111', '#222222'])
  })

  test('a custom palette accepts 12 colors and rejects 13', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ app: {} }))

    fireEvent.change(screen.getByLabelText('Chart palette'), { target: { value: 'custom' } })
    const colors = Array.from({ length: 13 }, (_, i) => `#${i.toString(16).padStart(6, '0')}`)
    fireEvent.change(screen.getByLabelText('Custom chart colors'), { target: { value: colors.slice(0, 12).join(', ') } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    expect((savedApp(bodies).theme as Record<string, unknown>).chart_palette).toEqual(colors.slice(0, 12))

    fireEvent.change(screen.getByLabelText('Custom chart colors'), { target: { value: colors.join(', ') } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Chart palettes support at most 12 colors')
    expect(bodies.length).toBe(1)
  })

  test('the radius persists as a number', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ app: { theme: { radius: 4 } } }))

    fireEvent.change(screen.getByLabelText('Corner radius'), { target: { value: '16' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    const theme = savedApp(bodies).theme as Record<string, unknown>
    expect(theme.radius).toBe(16)
    expect(typeof theme.radius).toBe('number')
  })

  test('clearing a color removes that key from the theme', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ app: { theme: { accent: '#ff0000', radius: 4 } } }))

    fireEvent.change(screen.getByLabelText('Accent'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    expect(savedApp(bodies).theme).toEqual({ radius: 4 })
  })

  test('clearing every theme field sends theme: null', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({
      app: {
        theme: {
          accent: '#ff0000',
          background: '#ffffff',
          surface: '#fafafa',
          text: '#111111',
          border: '#eeeeee',
          radius: 6,
          font_family: 'jetbrains-mono',
          color_scheme: 'dark',
          chart_palette: ['#111111'],
        },
      },
    }))

    for (const label of ['Accent', 'Background', 'Surface', 'Text', 'Border']) {
      fireEvent.change(screen.getByLabelText(label), { target: { value: '' } })
    }
    fireEvent.change(screen.getByLabelText('Corner radius'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Font family'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Color scheme'), { target: { value: 'auto' } })
    fireEvent.change(screen.getByLabelText('Chart palette'), { target: { value: 'default' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    const app = savedApp(bodies)
    expect('theme' in app).toBe(true)
    expect(app.theme).toBeNull()
  })

  test('an out-of-range radius is rejected before saving', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ app: {} }))

    fireEvent.change(screen.getByLabelText('Corner radius'), { target: { value: '30' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Corner radius must be a whole number between 0 and 24')
    expect(bodies.length).toBe(0)
  })

  test('an invalid color is rejected before saving', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ app: {} }))

    fireEvent.change(screen.getByLabelText('Accent'), { target: { value: 'red' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Accent must be a hex color like #6366f1')
    expect(bodies.length).toBe(0)
  })

  test('a 3-digit hex fills the color swatch', () => {
    renderPanel(dashboardWith({ app: { theme: { accent: '#fff' } } }))
    expect((screen.getByLabelText('Accent picker') as HTMLInputElement).value).toBe('#ffffff')
  })

  test('uploading a logo POSTs to the dashboard attachments endpoint and saves the id', async () => {
    const bodies = capturePuts()
    const uploads: Array<{ auth: string | null; adminMode: string | null; type: string | null }> = []
    server.use(
      http.post('/api/v1/dashboards/d1/attachments', async ({ request }) => {
        const form = await request.formData()
        const file = form.get('file') as File | null
        uploads.push({
          auth: request.headers.get('Authorization'),
          adminMode: request.headers.get('X-AETHER-Admin-Mode'),
          type: file?.type ?? null,
        })
        return HttpResponse.json(
          { id: 'att-9', filename: 'logo.png', mime_type: 'image/png', size: 3 },
          { status: 201 },
        )
      }),
    )
    const appendSpy = vi.spyOn(FormData.prototype, 'append')
    localStorage.setItem('aether_token', 'test-token')
    localStorage.setItem('aether_admin_mode', 'true')
    try {
      renderPanel(dashboardWith({ app: {} }, { logo_attachment_id: 'org-logo' }))
      // No dashboard logo yet: the org logo is the effective fallback.
      expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/org-logo?token=test-token')

      fireEvent.click(screen.getByLabelText('Upload…'))
      const file = new File(['png'], 'logo.png', { type: 'image/png' })
      fireEvent.change(screen.getByLabelText('Logo file'), { target: { files: [file] } })

      await waitFor(() => expect(uploads.length).toBe(1))
      expect(uploads[0]).toEqual({
        auth: 'Bearer test-token',
        adminMode: 'true',
        type: 'image/png',
      })
      // jsdom's FormData → undici bridge mangles the part on the wire (name
      // becomes "blob", bytes become the string "undefined"), so the panel's
      // append call is the authoritative check that the chosen file (name and
      // type) goes into the `file` field.
      const fileCall = appendSpy.mock.calls.find(([field]) => field === 'file')
      expect(fileCall?.[1]).toBe(file)
      expect((fileCall?.[1] as File).name).toBe('logo.png')
      expect((fileCall?.[1] as File).type).toBe('image/png')
      await waitFor(() =>
        expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/att-9?token=test-token'),
      )

      fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))
      await waitFor(() => expect(bodies.length).toBe(1))
      expect(savedApp(bodies).logo_attachment_id).toBe('att-9')
    } finally {
      appendSpy.mockRestore()
      localStorage.removeItem('aether_token')
      localStorage.removeItem('aether_admin_mode')
    }
  })

  test('the logo source radios are disabled while an upload is in flight', async () => {
    let started = false
    let release: (() => void) | undefined
    server.use(
      http.post('/api/v1/dashboards/d1/attachments', async () => {
        started = true
        await new Promise<void>((resolve) => { release = resolve })
        return HttpResponse.json(
          { id: 'att-9', filename: 'logo.png', mime_type: 'image/png', size: 3 },
          { status: 201 },
        )
      }),
    )
    localStorage.setItem('aether_token', 'test-token')
    try {
      renderPanel(dashboardWith({ app: {} }, { logo_attachment_id: 'org-logo' }))
      fireEvent.click(screen.getByLabelText('Upload…'))
      fireEvent.change(screen.getByLabelText('Logo file'), {
        target: { files: [new File(['png'], 'logo.png', { type: 'image/png' })] },
      })

      await waitFor(() => expect(started).toBe(true))
      expect(screen.getByLabelText('None')).toBeDisabled()
      expect(screen.getByLabelText('Use org logo')).toBeDisabled()
      expect(screen.getByLabelText('Upload…')).toBeDisabled()

      release?.()
      await waitFor(() => expect(screen.getByLabelText('Upload…')).not.toBeDisabled())
      await waitFor(() =>
        expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/att-9?token=test-token'),
      )
    } finally {
      localStorage.removeItem('aether_token')
    }
  })

  test('a failed logo upload surfaces the error and keeps the fallback', async () => {
    server.use(
      http.post('/api/v1/dashboards/d1/attachments', () =>
        HttpResponse.json({ error: 'storage error' }, { status: 500 }),
      ),
    )
    localStorage.setItem('aether_token', 'test-token')
    try {
      renderPanel(dashboardWith({ app: {} }, { logo_attachment_id: 'org-logo' }))
      fireEvent.click(screen.getByLabelText('Upload…'))
      fireEvent.change(screen.getByLabelText('Logo file'), {
        target: { files: [new File(['png'], 'logo.png', { type: 'image/png' })] },
      })

      expect(await screen.findByRole('alert')).toHaveTextContent('storage error')
      expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/org-logo?token=test-token')
    } finally {
      localStorage.removeItem('aether_token')
    }
  })

  test('"None" clears the dashboard logo and previews the org fallback', async () => {
    const bodies = capturePuts()
    localStorage.setItem('aether_token', 'test-token')
    try {
      renderPanel(dashboardWith(
        { app: { logo_attachment_id: 'att-dash' } },
        { logo_attachment_id: 'org-logo' },
      ))
      expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/att-dash?token=test-token')

      fireEvent.click(screen.getByLabelText('None'))
      await waitFor(() =>
        expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/org-logo?token=test-token'),
      )

      fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))
      await waitFor(() => expect(bodies.length).toBe(1))
      expect('logo_attachment_id' in savedApp(bodies)).toBe(false)
    } finally {
      localStorage.removeItem('aether_token')
    }
  })

  test('"Use org logo" clears the dashboard logo and previews the org fallback', async () => {
    const bodies = capturePuts()
    localStorage.setItem('aether_token', 'test-token')
    try {
      renderPanel(dashboardWith(
        { app: { logo_attachment_id: 'att-dash' } },
        { logo_attachment_id: 'org-logo' },
      ))
      expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/att-dash?token=test-token')

      fireEvent.click(screen.getByLabelText('Use org logo'))
      await waitFor(() =>
        expect(screen.getByAltText('Logo preview')).toHaveAttribute('src', '/api/v1/attachments/org-logo?token=test-token'),
      )

      fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))
      await waitFor(() => expect(bodies.length).toBe(1))
      expect('logo_attachment_id' in savedApp(bodies)).toBe(false)
    } finally {
      localStorage.removeItem('aether_token')
    }
  })

  test('renders the Tabs section with the stored chapters', () => {
    renderPanel(dashboardWith({
      tabs: [
        { id: 'overview', title: 'Overview' },
        { id: 'details', title: 'Details' },
      ],
    }))
    expect(screen.getByRole('heading', { name: 'Tabs', level: 3 })).toBeInTheDocument()
    expect((screen.getByLabelText('Tab 1 title') as HTMLInputElement).value).toBe('Overview')
    expect((screen.getByLabelText('Tab 2 title') as HTMLInputElement).value).toBe('Details')
    expect(screen.getByRole('button', { name: 'Remove Overview' })).toBeInTheDocument()
  })

  test('adding tabs slugs ids from the default title and saves them', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({}))

    fireEvent.click(screen.getByRole('button', { name: 'Add tab' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add tab' }))
    expect(screen.getByLabelText('Tab 1 title')).toHaveValue('Tab 1')
    expect(screen.getByLabelText('Tab 2 title')).toHaveValue('Tab 2')

    fireEvent.change(screen.getByLabelText('Tab 1 title'), { target: { value: 'Overview' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    // The id was slugged at add time and did not follow the rename: widgets
    // reference the id, so renaming a chapter must not strand them.
    expect((bodies[0].settings as Record<string, unknown>).tabs).toEqual([
      { id: 'tab-1', title: 'Overview' },
      { id: 'tab-2', title: 'Tab 2' },
    ])
  })

  test('a new tab id gets a uniqueness suffix when the slug is taken', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({ tabs: [{ id: 'tab-1', title: 'Overview' }] }))

    fireEvent.click(screen.getByRole('button', { name: 'Add tab' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    expect((bodies[0].settings as Record<string, unknown>).tabs).toEqual([
      { id: 'tab-1', title: 'Overview' },
      { id: 'tab-1-2', title: 'Tab 1' },
    ])
  })

  test('renaming and moving tabs reorders the saved list', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({
      tabs: [
        { id: 'a', title: 'A' },
        { id: 'b', title: 'B' },
      ],
    }))

    fireEvent.change(screen.getByLabelText('Tab 1 title'), { target: { value: 'Alpha' } })
    fireEvent.click(screen.getByRole('button', { name: 'Move Alpha down' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    expect((bodies[0].settings as Record<string, unknown>).tabs).toEqual([
      { id: 'b', title: 'B' },
      { id: 'a', title: 'Alpha' },
    ])
  })

  test('disables Add tab at the server limit of ten', () => {
    renderPanel(dashboardWith({
      tabs: Array.from({ length: 10 }, (_, i) => ({ id: `t${i}`, title: `T${i}` })),
    }))
    expect(screen.getByRole('button', { name: 'Add tab' })).toBeDisabled()
  })

  test('deleting a tab clears its widgets over REST without a document, then removes it', async () => {
    const clears: Array<{ widgetId: string; body: Record<string, unknown> }> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/:widgetId', async ({ request, params }) => {
        clears.push({
          widgetId: String(params.widgetId),
          body: (await request.json()) as Record<string, unknown>,
        })
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const bodies = capturePuts()
    renderPanel(
      dashboardWith({
        tabs: [
          { id: 'a', title: 'A' },
          { id: 'b', title: 'B' },
        ],
      }),
      vi.fn(),
      vi.fn(),
      {
        widgets: [
          widget({ id: 'w1', layout: { row: 1, col: 2, width: 3, height: 4, tab: 'a' } }),
          widget({ id: 'w2', layout: { row: 0, col: 0, width: 6, height: 6, tab: 'b' } }),
          widget({ id: 'w3' }),
        ],
      },
    )

    fireEvent.click(screen.getByRole('button', { name: 'Remove A' }))

    await waitFor(() => expect(clears.length).toBe(1))
    expect(clears[0]).toEqual({
      widgetId: 'w1',
      body: { layout: { row: 1, col: 2, width: 3, height: 4, tab: '' } },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))
    await waitFor(() => expect(bodies.length).toBe(1))
    expect((bodies[0].settings as Record<string, unknown>).tabs).toEqual([{ id: 'b', title: 'B' }])
  })

  test('deleting a tab clears its widgets through the live document when bound', async () => {
    const entry = { doc: {}, provider: {}, refCount: 1, synced: true, connected: true }
    vi.mocked(peekDashboardCollab).mockReturnValue(entry as never)
    renderPanel(
      dashboardWith({ tabs: [{ id: 'a', title: 'A' }] }),
      vi.fn(),
      vi.fn(),
      { widgets: [widget({ id: 'w1', layout: { row: 0, col: 0, width: 6, height: 6, tab: 'a' } })] },
    )

    fireEvent.click(screen.getByRole('button', { name: 'Remove A' }))

    await waitFor(() => expect(setWidgetTab).toHaveBeenCalledWith(entry, 'w1', ''))
  })

  test('initialSection tabs reveals and focuses the first title input', () => {
    renderPanel(
      dashboardWith({ tabs: [{ id: 'a', title: 'A' }] }),
      vi.fn(),
      vi.fn(),
      { initialSection: 'tabs' },
    )
    expect(screen.getByLabelText('Tab 1 title')).toHaveFocus()
  })

  test('initialSection tabs focuses Add tab when there are no chapters yet', () => {
    renderPanel(dashboardWith({}), vi.fn(), vi.fn(), { initialSection: 'tabs' })
    expect(screen.getByRole('button', { name: 'Add tab' })).toHaveFocus()
  })
})
