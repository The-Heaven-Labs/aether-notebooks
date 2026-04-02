import type { Meta, StoryObj } from '@storybook/react-vite'
import { Check, X } from 'lucide-react'
import { StatusBadge } from './StatusBadge'

const meta: Meta<typeof StatusBadge> = {
  title: 'Components/StatusBadge',
  component: StatusBadge,
}

export default meta

type Story = StoryObj<typeof StatusBadge>

export const Success: Story = {
  args: {
    status: 'success',
    label: 'Connected',
    icon: <Check size={12} />,
  },
}

export const Error: Story = {
  args: {
    status: 'error',
    label: 'Connection failed',
    icon: <X size={12} />,
  },
}

export const Neutral: Story = {
  args: {
    status: 'neutral',
    label: '—',
  },
}

export const NoIcon: Story = {
  args: {
    status: 'success',
    label: 'Enabled',
  },
}
