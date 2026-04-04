import type { Meta, StoryObj } from '@storybook/react-vite'
import { CellVariantHex } from './CellVariantHex'
import type { Cell, Connector } from '../types'

const meta: Meta<typeof CellVariantHex> = {
  component: CellVariantHex,
  title: 'Variants/Cell — Hex Style (Option A)',
  parameters: {
    layout: 'padded',
    docs: {
      description: {
        component:
          'Hex-inspired cell variant: 4px left-rail type indicator, hover-only floating toolbar, ' +
          'run button circle on the rail. Inspired by hex.tech notebook UI.',
      },
    },
  },
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 800, margin: '0 auto' }}>
        <Story />
      </div>
    ),
  ],
}
export default meta
type Story = StoryObj<typeof CellVariantHex>

// ── Shared fixtures ───────────────────────────────────────────────────────────

const baseCell: Cell = {
  id: 'cell-1',
  notebook_id: 'nb-1',
  type: 'code',
  language: 'sql',
  source: 'SELECT account_id, SUM(amount) AS revenue\nFROM orders\nGROUP BY 1\nORDER BY 2 DESC',
  outputs: [],
  position: 0,
  source_visible: true,
  cell_collapsed: false,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
  title: 'Revenue by Account',
  description: '',
  slug: '',
}

const connector: Connector = {
  id: 'conn-1',
  name: 'Postgres prod',
  type: 'postgres',
  created_at: '2024-01-01T00:00:00Z',
}

const sqlSource = `SELECT
  account_id,
  SUM(amount)  AS revenue,
  COUNT(*)     AS orders
FROM orders
WHERE created_at >= '2025-01-01'
GROUP BY 1
ORDER BY 2 DESC
LIMIT 50`

const outputData = {
  columns: ['account_id', 'revenue', 'orders'],
  rows: [
    ['acct-001', '$48,200', '312'],
    ['acct-019', '$36,750', '241'],
    ['acct-007', '$31,100', '198'],
    ['acct-042', '$24,880', '167'],
    ['acct-011', '$19,350', '134'],
  ],
}

const savedState = {
  saving: false,
  savedAt: new Date(Date.now() - 2 * 60 * 1000),
  error: null,
}

// ── Stories ───────────────────────────────────────────────────────────────────

export const Default: Story = {
  name: 'SQL — Idle (hover to see toolbar + run button)',
  args: {
    cell: baseCell,
    connectors: [connector],
    staticSource: sqlSource,
    running: false,
    saveState: savedState,
    onRun: () => {},
    onDelete: () => {},
    onMoveUp: () => {},
    onMoveDown: () => {},
    onSwitchType: () => {},
    onToggleSourceVisible: () => {},
    onToggleCellCollapsed: () => {},
    onShowHistory: () => {},
    onUpdateCellMeta: () => {},
  },
}

export const Running: Story = {
  name: 'SQL — Running',
  args: {
    ...Default.args,
    running: true,
    saveState: { saving: true, savedAt: null, error: null },
  },
}

export const WithOutput: Story = {
  name: 'SQL — With Results',
  args: {
    ...Default.args,
    staticOutput: outputData,
    runAt: new Date(Date.now() - 30 * 1000),
  },
}

export const SourceHidden: Story = {
  name: 'SQL — Source Hidden (output only)',
  args: {
    ...Default.args,
    sourceVisible: false,
    staticOutput: outputData,
    runAt: new Date(Date.now() - 5 * 60 * 1000),
  },
}

export const NoConnector: Story = {
  name: 'SQL — No Connector Assigned',
  args: {
    ...Default.args,
    cell: { ...baseCell, connector_id: undefined, title: 'Scratch query' },
    connectors: [connector],
  },
}

export const SaveError: Story = {
  name: 'SQL — Save Error',
  args: {
    ...Default.args,
    saveState: { saving: false, savedAt: null, error: 'connection timeout' },
  },
}

export const MarkdownCell: Story = {
  name: 'Markdown — Text cell (blue rail)',
  args: {
    cell: {
      ...baseCell,
      id: 'cell-md',
      type: 'text',
      language: 'markdown',
      title: 'Analysis Notes',
      connector_id: undefined,
    },
    staticSource: `## Revenue Analysis — Q1 2025

Top accounts by revenue continue to be dominated by **enterprise** tier.
Notable: acct-042 grew **+34%** QoQ driven by new seat expansion.

> Review with Sales on 2025-02-03 for renewal strategy.`,
    saveState: savedState,
    onDelete: () => {},
    onMoveUp: () => {},
    onMoveDown: () => {},
    onSwitchType: () => {},
    onToggleSourceVisible: () => {},
    onToggleCellCollapsed: () => {},
    onShowHistory: () => {},
    onUpdateCellMeta: () => {},
  },
}

export const Collapsed: Story = {
  name: 'Collapsed — SQL cell',
  args: {
    cell: { ...baseCell, cell_collapsed: true, title: 'Revenue by Account' },
    connectors: [connector],
    onToggleCellCollapsed: () => {},
  },
}

export const CollapsedMarkdown: Story = {
  name: 'Collapsed — Markdown cell',
  args: {
    cell: {
      ...baseCell,
      id: 'cell-md-c',
      type: 'text',
      language: 'markdown',
      cell_collapsed: true,
      title: 'Analysis Notes',
      connector_id: undefined,
    },
    onToggleCellCollapsed: () => {},
  },
}

export const NoTitle: Story = {
  name: 'SQL — No title (connector badge only)',
  args: {
    ...Default.args,
    cell: { ...baseCell, title: undefined },
  },
}

export const FullNotebook: Story = {
  name: 'Full Notebook — Multiple cells stacked',
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 800, margin: '0 auto', display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Story />
      </div>
    ),
  ],
  render: () => (
    <>
      <CellVariantHex
        cell={{ ...baseCell, id: 'c1', title: 'Setup: date range' }}
        connectors={[connector]}
        staticSource={`SET search_path = analytics;\n-- Date range: last 90 days\nSET app.start_date = (NOW() - INTERVAL '90 days')::date;`}
        saveState={savedState}
        onRun={() => {}} onDelete={() => {}} onMoveUp={() => {}} onMoveDown={() => {}}
        onSwitchType={() => {}} onToggleSourceVisible={() => {}} onToggleCellCollapsed={() => {}} onShowHistory={() => {}} onUpdateCellMeta={() => {}}
      />
      <CellVariantHex
        cell={{
          ...baseCell, id: 'c2', type: 'text', language: 'markdown',
          title: 'Revenue Analysis', connector_id: undefined,
        }}
        staticSource={`## Revenue by Account\n\nCompares total revenue and order count across accounts in the selected window.`}
        saveState={savedState}
        onDelete={() => {}} onMoveUp={() => {}} onMoveDown={() => {}}
        onSwitchType={() => {}} onToggleSourceVisible={() => {}} onToggleCellCollapsed={() => {}} onShowHistory={() => {}} onUpdateCellMeta={() => {}}
      />
      <CellVariantHex
        cell={{ ...baseCell, id: 'c3', title: 'Revenue by Account' }}
        connectors={[connector]}
        staticSource={sqlSource}
        staticOutput={outputData}
        saveState={savedState}
        runAt={new Date(Date.now() - 45 * 1000)}
        onRun={() => {}} onDelete={() => {}} onMoveUp={() => {}} onMoveDown={() => {}}
        onSwitchType={() => {}} onToggleSourceVisible={() => {}} onToggleCellCollapsed={() => {}} onShowHistory={() => {}} onUpdateCellMeta={() => {}}
      />
      <CellVariantHex
        cell={{ ...baseCell, id: 'c4', cell_collapsed: true, title: 'MoM growth (archived)' }}
        connectors={[connector]}
        onToggleCellCollapsed={() => {}}
      />
    </>
  ),
}
