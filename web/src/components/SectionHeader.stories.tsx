import type { Meta, StoryObj } from '@storybook/react-vite'
import { SectionHeader } from './SectionHeader'

const meta: Meta<typeof SectionHeader> = {
  component: SectionHeader,
}

export default meta
type Story = StoryObj<typeof SectionHeader>

export const WithActions: Story = {
  args: {
    title: 'Notebooks',
    subtitle: '3 notebooks',
    children: (
      <button style={{ padding: '6px 16px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer' }}>
        + New Notebook
      </button>
    ),
  },
}

export const WithInput: Story = {
  args: {
    title: 'Audit Log',
    subtitle: '42 entries loaded',
    children: (
      <input style={{ padding: '8px 12px', border: '1.5px solid var(--border)', borderRadius: 6, fontSize: 13, background: 'var(--bg-primary)' }} placeholder="Filter by action…" />
    ),
  },
}

export const NoChildren: Story = {
  args: {
    title: 'Members',
    subtitle: '5 members',
  },
}
