import type { Meta, StoryObj } from '@storybook/react-vite'
import { ConnectorsVariantCard } from './ConnectorsVariantCard'

const meta: Meta<typeof ConnectorsVariantCard> = {
  component: ConnectorsVariantCard,
  title: 'Variants/Connectors — Card Style',
  parameters: {
    layout: 'padded',
    docs: {
      description: {
        component:
          'Card/accordion-style connector list inspired by the GroupsPage design. ' +
          'Each connector is an expandable card showing connection details. ' +
          'Compare to the current StyledTable approach at /connectors.',
      },
    },
  },
  decorators: [(Story) => <div style={{ maxWidth: 800, margin: '0 auto' }}><Story /></div>],
}
export default meta
type Story = StoryObj<typeof ConnectorsVariantCard>

const fixtures = [
  { id: 'c1', name: 'Production DB', type: 'postgres', is_default: true, host: 'db.prod.internal', port: 5432, database: 'hnb_prod', user: 'app_user' },
  { id: 'c2', name: 'Analytics', type: 'clickhouse', is_default: false, host: 'ch.analytics.internal', port: 9000, database: 'events', user: 'readonly' },
  { id: 'c3', name: 'Staging', type: 'postgres', is_default: false, host: 'db.staging.internal', port: 5432, database: 'hnb_staging', user: 'app_user' },
]

export const Viewer: Story = {
  args: { connectors: fixtures, isAdmin: false },
  name: 'Viewer (read-only)',
}

export const Admin: Story = {
  args: {
    connectors: fixtures,
    isAdmin: true,
    onEdit: (id) => alert(`Edit ${id}`),
    onDelete: (id) => alert(`Delete ${id}`),
    onSetDefault: (id) => alert(`Set default ${id}`),
  },
  name: 'Admin (with actions)',
}

export const Empty: Story = {
  args: { connectors: [], isAdmin: true },
  name: 'Empty state',
}
