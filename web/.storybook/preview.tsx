import type { Preview } from '@storybook/react-vite'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import '../src/styles/theme.css'

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})

const preview: Preview = {
  decorators: [
    (Story) => (
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <Story />
        </QueryClientProvider>
      </MemoryRouter>
    ),
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
      test: 'todo',
    },
  },
}

export default preview
