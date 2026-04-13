import type { Meta, StoryObj } from '@storybook/react-vite'
import { MembersVariantCard } from './MembersVariantCard'

const meta: Meta<typeof MembersVariantCard> = {
  component: MembersVariantCard,
  title: 'Variants/Members — Card Style',
  parameters: {
    layout: 'padded',
    docs: {
      description: {
        component:
          'Card-based member list in the GroupsPage visual style. ' +
          'Compare to the current StyledTable approach at /members. ' +
          'Avatar initials, inline role select for admins, joined date.',
      },
    },
  },
  decorators: [(Story) => <div style={{ maxWidth: 800, margin: '0 auto' }}><Story /></div>],
}
export default meta
type Story = StoryObj<typeof MembersVariantCard>

const fixtures: Parameters<typeof MembersVariantCard>[0]['members'] = [
  { user_id: 'u1', name: 'Alice Chen', email: 'alice@example.com', role: 'admin', joined_at: '2025-11-01T00:00:00Z' },
  { user_id: 'u2', name: 'Bob Torres', email: 'bob@example.com', role: 'editor', joined_at: '2026-01-15T00:00:00Z' },
  { user_id: 'u3', name: '', email: 'carol@example.com', role: 'viewer', joined_at: '2026-03-20T00:00:00Z' },
]

export const Viewer: Story = {
  args: { members: fixtures, currentUserId: 'u1', isAdmin: false },
  name: 'Viewer (read-only)',
}

export const Admin: Story = {
  args: {
    members: fixtures,
    currentUserId: 'u1',
    isAdmin: true,
    onRoleChange: (id, r) => alert(`Role ${id} → ${r}`),
    onRemove: (id) => alert(`Remove ${id}`),
  },
  name: 'Admin (with actions)',
}

export const Empty: Story = {
  args: { members: [], isAdmin: true },
  name: 'Empty state',
}
