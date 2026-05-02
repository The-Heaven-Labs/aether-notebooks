import { screen } from '@testing-library/react'
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
