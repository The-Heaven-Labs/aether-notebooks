import type { ReactNode } from 'react'
import { render } from '@testing-library/react'
import type { RenderOptions } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthContext } from '../hooks/useAuth'

interface AuthUser {
  user_id: string
  org_id: string
  role: string
}

const DEFAULT_USER: AuthUser = {
  user_id: 'user-1',
  org_id: 'org-1',
  role: 'admin',
}

interface ProviderOptions extends RenderOptions {
  user?: AuthUser | null
  initialPath?: string
}

export function renderWithProviders(
  ui: ReactNode,
  { user = DEFAULT_USER, initialPath = '/', ...renderOptions }: ProviderOptions = {}
) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  const authValue = {
    user,
    isAuthenticated: !!user,
    login: async () => {},
    register: async () => null,
    logout: () => {},
    loginWithToken: (_token: string) => {},
  }

  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <AuthContext.Provider value={authValue}>
          <MemoryRouter initialEntries={[initialPath]}>
            {children}
          </MemoryRouter>
        </AuthContext.Provider>
      </QueryClientProvider>
    )
  }

  return render(ui, { wrapper: Wrapper, ...renderOptions })
}

export function editorUser(): AuthUser {
  return { user_id: 'user-2', org_id: 'org-1', role: 'editor' }
}

export function viewerUser(): AuthUser {
  return { user_id: 'user-3', org_id: 'org-1', role: 'viewer' }
}
