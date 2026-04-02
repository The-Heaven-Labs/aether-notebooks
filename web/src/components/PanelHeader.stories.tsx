import type { Meta, StoryObj } from '@storybook/react-vite'
import { PanelHeader } from './PanelHeader'

const meta: Meta<typeof PanelHeader> = {
  component: PanelHeader,
  title: 'Components/PanelHeader',
}
export default meta
type Story = StoryObj<typeof PanelHeader>

export const WithClose: Story = {
  args: {
    title: 'Schema Browser',
    onClose: () => {},
    closeTitle: 'Close schema browser',
  },
}

export const WithoutClose: Story = {
  args: {
    title: 'Schedules',
  },
}

export const CustomStyle: Story = {
  args: {
    title: 'Cell History',
    onClose: () => {},
    style: {
      background: 'white',
      borderBottom: '1px solid var(--border-light)',
    },
  },
}
