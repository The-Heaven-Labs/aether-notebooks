import type { Meta, StoryObj } from '@storybook/react-vite'
import { TreeItem } from './TreeItem'

const meta: Meta<typeof TreeItem> = {
  component: TreeItem,
}

export default meta

type Story = StoryObj<typeof TreeItem>

export const Collapsed: Story = {
  args: {
    name: 'users',
    columns: [
      { name: 'id', type: 'uuid' },
      { name: 'email', type: 'text' },
      { name: 'created_at', type: 'timestamp' },
    ],
    isExpanded: false,
    onToggle: () => {},
  },
}

export const Expanded: Story = {
  args: {
    name: 'users',
    columns: [
      { name: 'id', type: 'uuid' },
      { name: 'email', type: 'text' },
      { name: 'created_at', type: 'timestamp' },
    ],
    isExpanded: true,
    onToggle: () => {},
  },
}

export const ManyColumns: Story = {
  args: {
    name: 'events',
    columns: [
      { name: 'id', type: 'uuid' },
      { name: 'user_id', type: 'uuid' },
      { name: 'type', type: 'text' },
      { name: 'payload', type: 'jsonb' },
      { name: 'created_at', type: 'timestamp' },
      { name: 'updated_at', type: 'timestamp' },
      { name: 'session_id', type: 'uuid' },
      { name: 'ip_address', type: 'inet' },
    ],
    isExpanded: true,
    onToggle: () => {},
  },
}
