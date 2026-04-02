import type { Meta, StoryObj } from '@storybook/react-vite'
import { CellHeader } from './CellHeader'
import type { Cell } from '../types'

const meta: Meta<typeof CellHeader> = {
  component: CellHeader,
  title: 'Components/CellHeader',
}
export default meta
type Story = StoryObj<typeof CellHeader>

const baseCell: Cell = {
  id: 'cell-1',
  notebook_id: 'nb-1',
  type: 'code',
  language: 'sql',
  source: 'SELECT 1',
  outputs: [],
  position: 0,
  source_visible: true,
  cell_collapsed: false,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
  title: '',
  description: '',
  slug: '',
}

export const WithTitle: Story = {
  args: {
    cell: { ...baseCell, title: 'Revenue by Month' },
    onUpdateCell: () => {},
  },
}

export const WithTitleAndSlug: Story = {
  args: {
    cell: { ...baseCell, title: 'Revenue by Month', slug: 'revenue_by_month' },
    onUpdateCell: () => {},
  },
}

export const WithDescription: Story = {
  args: {
    cell: {
      ...baseCell,
      title: 'Active Users',
      description: 'Count of users who logged in during the past 30 days.',
    },
    onUpdateCell: () => {},
  },
}

export const WithReferencedBy: Story = {
  args: {
    cell: { ...baseCell, title: 'Connector Filter', slug: 'connector_id' },
    onUpdateCell: () => {},
    referencedByCount: 4,
  },
}
