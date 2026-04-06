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
      <button style={{ padding: '6px 16px', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer' }}>
        + New Notebook
      </button>
    ),
  },
}

export const WithActionsDark: Story = {
  ...WithActions,
  name: 'With Actions — Dark',
  parameters: { theme: 'dark' },
}

export const WithInput: Story = {
  args: {
    title: 'Audit Log',
    subtitle: '42 entries loaded',
    children: (
      <input style={{ padding: '8px 12px', border: '1.5px solid var(--border)', borderRadius: 6, fontSize: 13, background: 'var(--bg-primary)', color: 'var(--text-primary)' }} placeholder="Filter by action…" />
    ),
  },
}

export const WithInputDark: Story = {
  ...WithInput,
  name: 'With Input — Dark',
  parameters: { theme: 'dark' },
}

export const NoChildren: Story = {
  args: {
    title: 'Members',
    subtitle: '5 members',
  },
}

export const NoChildrenDark: Story = {
  ...NoChildren,
  name: 'No Children — Dark',
  parameters: { theme: 'dark' },
}
