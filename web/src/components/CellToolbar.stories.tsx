import type { Meta, StoryObj } from '@storybook/react-vite'
import { CellToolbar } from './CellToolbar'
import type { Connector } from '../types'

const meta: Meta<typeof CellToolbar> = {
  component: CellToolbar,
  title: 'Components/CellToolbar',
}
export default meta
type Story = StoryObj<typeof CellToolbar>

const connectors: Connector[] = [
  { id: 'conn-1', name: 'Production DB', type: 'postgres', created_at: '2024-01-01T00:00:00Z' },
  { id: 'conn-2', name: 'Analytics', type: 'clickhouse', created_at: '2024-01-01T00:00:00Z' },
]

const baseArgs = {
  onRun: () => {},
  onDelete: () => {},
  onMoveUp: () => {},
  onMoveDown: () => {},
  onSwitchType: () => {},
  onAssignConnector: () => {},
  onClearConnector: () => {},
  onToggleSourceVisible: () => {},
  onToggleCellCollapsed: () => {},
  onShowHistory: () => {},
  running: false,
  cellType: 'code' as const,
  sourceVisible: true,
  cellCollapsed: false,
  connectors,
}

export const Default: Story = {
  args: baseArgs,
}

export const Running: Story = {
  args: { ...baseArgs, running: true },
}

export const WithConnectorAssigned: Story = {
  args: { ...baseArgs, connectorId: 'conn-1' },
}

export const SourceHidden: Story = {
  args: { ...baseArgs, sourceVisible: false },
}

export const TextCell: Story = {
  args: { ...baseArgs, cellType: 'text' as const },
}
