import type { Meta, StoryObj } from '@storybook/react-vite'
import { ErrorBanner } from './ErrorBanner'

const meta: Meta<typeof ErrorBanner> = {
  component: ErrorBanner,
}

export default meta
type Story = StoryObj<typeof ErrorBanner>

export const Error: Story = {
  args: {
    message: 'Failed to load data. Please try again.',
    variant: 'error',
  },
}

export const ErrorWithDismiss: Story = {
  args: {
    message: 'An error occurred while saving your changes.',
    variant: 'error',
    onDismiss: () => {},
  },
}

export const Warning: Story = {
  args: {
    message: 'This action cannot be undone.',
    variant: 'warning',
  },
}

export const Info: Story = {
  args: {
    message: 'Your session will expire in 5 minutes.',
    variant: 'info',
  },
}