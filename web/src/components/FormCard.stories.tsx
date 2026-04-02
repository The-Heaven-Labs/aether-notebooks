import type { Meta, StoryObj } from '@storybook/react-vite'
import { FormCard } from './FormCard'

const meta: Meta<typeof FormCard> = {
  title: 'Components/FormCard',
  component: FormCard,
}

export default meta
type Story = StoryObj<typeof FormCard>

export const Default: Story = {
  args: {
    title: 'New Connector',
    children: (
      <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Form fields go here.</p>
    ),
  },
}

export const WithActions: Story = {
  args: {
    title: 'Invite Member',
    children: (
      <div>
        <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
          <input
            type="email"
            placeholder="colleague@example.com"
            style={{ flex: 1, padding: '7px 12px', border: '1px solid var(--border)', borderRadius: 6, fontSize: 13 }}
          />
          <select style={{ padding: '7px 10px', border: '1px solid var(--border)', borderRadius: 6, fontSize: 13 }}>
            <option>Viewer</option>
            <option>Editor</option>
            <option>Admin</option>
          </select>
          <button type="button" style={{ padding: '7px 18px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer' }}>
            Invite
          </button>
        </div>
      </div>
    ),
  },
}
