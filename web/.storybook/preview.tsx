import type { Preview } from '@storybook/react-vite'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { useEffect } from 'react'
import '../src/styles/theme.css'

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})

function ThemeSetter({ theme }: { theme: string }) {
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    return () => document.documentElement.removeAttribute('data-theme')
  }, [theme])
  return null
}

const preview: Preview = {
  decorators: [
    (Story, context) => {
      const theme = context.parameters['theme'] ?? 'light'
      return (
        <MemoryRouter>
          <QueryClientProvider client={queryClient}>
            <ThemeSetter theme={theme} />
            <Story />
          </QueryClientProvider>
        </MemoryRouter>
      )
    },
  ],
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    backgrounds: {
      default: 'hnb-light',
      values: [
        { name: 'hnb-light', value: '#f8f7f4' },
        { name: 'hnb-dark', value: '#1a1814' },
      ],
    },
    a11y: {
      test: 'error',
    },
  },
}

export default preview
