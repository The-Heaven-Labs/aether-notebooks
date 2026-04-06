import type { Meta, StoryObj } from '@storybook/react-vite'
import { EmptyState } from './EmptyState'

const meta: Meta<typeof EmptyState> = {
  component: EmptyState,
}

export default meta
type Story = StoryObj<typeof EmptyState>

export const WithIcon: Story = {
  args: {
    icon: <span>📓</span>,
    title: 'No notebooks yet',
    text: 'Create your first notebook to get started.',
    action: { label: 'Create notebook', onClick: () => {} },
  },
}

export const WithIconDark: Story = {
  ...WithIcon,
  name: 'With Icon — Dark',
  parameters: { theme: 'dark' },
}

export const WithoutIcon: Story = {
  args: {
    title: 'No dashboards yet',
    text: 'Create a dashboard to display results.',
    action: { label: '+ New Dashboard', onClick: () => {} },
  },
}

export const WithoutIconDark: Story = {
  ...WithoutIcon,
  name: 'Without Icon — Dark',
  parameters: { theme: 'dark' },
}

export const TextOnly: Story = {
  args: {
    title: 'No entries found',
    text: 'The audit log is empty.',
  },
}

export const TextOnlyDark: Story = {
  ...TextOnly,
  name: 'Text Only — Dark',
  parameters: { theme: 'dark' },
}
