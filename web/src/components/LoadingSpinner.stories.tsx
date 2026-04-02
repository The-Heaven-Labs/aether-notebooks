import type { Meta, StoryObj } from '@storybook/react-vite'
import { LoadingSpinner } from './LoadingSpinner'

const meta: Meta<typeof LoadingSpinner> = {
  component: LoadingSpinner,
}

export default meta
type Story = StoryObj<typeof LoadingSpinner>

export const Default: Story = {
  args: {},
}

export const Small: Story = {
  args: {
    size: 6,
  },
}

export const Large: Story = {
  args: {
    size: 16,
  },
}

export const Centered: Story = {
  args: {
    size: 10,
    style: { margin: '100px auto' },
  },
}