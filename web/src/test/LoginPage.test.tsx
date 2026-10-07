import { describe, it, expect } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { renderWithProviders } from './utils'
import { server } from './server'
import { LoginPage } from '../pages/LoginPage'

const PROVIDER = { id: 'p1', name: 'test-oidc', provider_type: 'oidc' }

describe('LoginPage', () => {
  it('focuses the first OIDC button when the SSO step appears', async () => {
    server.use(
      http.get('/api/v1/public/motd', () => HttpResponse.json([])),
      http.get('/api/v1/auth/sso-providers', () => HttpResponse.json([PROVIDER])),
    )
    renderWithProviders(<LoginPage />)

    // The live-session preview may render copies of the card; the real one is first.
    const emailInput = (await screen.findAllByPlaceholderText('you@example.com'))[0]
    fireEvent.change(emailInput, { target: { value: 'admin@heaven-labs.com' } })
    fireEvent.submit(emailInput.closest('form')!)

    const ssoButton = (await screen.findAllByRole('button', { name: 'Sign in with test-oidc' }))[0]
    await waitFor(() => expect(document.activeElement).toBe(ssoButton))
  })
})
