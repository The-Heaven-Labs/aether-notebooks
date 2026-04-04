import type { Meta, StoryObj } from '@storybook/react-vite'
import { CellVariantWireframe } from './CellVariantWireframe'
import type { Cell, Connector } from '../types'

const meta: Meta<typeof CellVariantWireframe> = {
  component: CellVariantWireframe,
  title: 'Variants/Cell — Wireframe (Option B)',
  parameters: {
    layout: 'padded',
    docs: {
      description: {
        component:
          'Hex-inspired minimalist/wireframe cell variant. No colored accents, no borders on the cell itself — ' +
          'flat gray code blocks, hairline table borders, hover-only plain icon actions. ' +
          'Wrap multiple cells in the NotebookShell decorator to get the full Hex white-card-on-dark-canvas look.',
      },
    },
  },
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 860, margin: '0 auto' }}>
        <Story />
      </div>
    ),
  ],
}
export default meta
type Story = StoryObj<typeof CellVariantWireframe>

// ── Fixtures ──────────────────────────────────────────────────────────────────

const baseCell: Cell = {
  id: 'cell-1',
  notebook_id: 'nb-1',
  type: 'code',
  language: 'sql',
  source: '',
  outputs: [],
  position: 0,
  source_visible: true,
  cell_collapsed: false,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
  title: 'Revenue by account',
  description: '',
  slug: '',
}

const connector: Connector = {
  id: 'conn-1',
  name: 'postgres-prod',
  type: 'postgres',
  created_at: '2024-01-01T00:00:00Z',
}

const sqlSource = `select
  account_id,
  sum(amount)  as revenue,
  count(*)     as orders
from orders
where created_at >= '2025-01-01'
group by 1
order by 2 desc
limit 50`

const clusterSource = `select * from "DEMO_DATA"."DEMOS"."CLOTHES_REVIEWS"`

const savedState = { saving: false, savedAt: new Date(Date.now() - 2 * 60 * 1000), error: null }

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

const clusterOutput = {
  columns: ['FIT', 'USER_ID', 'bust size', 'ITEM_ID', 'WEIGHT', 'RATING', 'rented_for', 'REVIEW_TEXT'],
  rows: [
    ['fit', '420439', '34d', '2031987', '146lbs', '10', 'other', 'I wore this to a dress rehearsal...'],
    ['fit', '756137', '34c', '724150', '125lbs', '10', 'wedding', 'This dress was awesome...'],
    ['fit', '993334', '34c', '864981', '146lbs', '10', 'other', 'I wore this dress past for our engagement...'],
    ['small', '642933', '34b+', '641589', 'None', '6', 'wedding', 'It runs small, but neat it is a super...'],
  ],
}

const shared = {
  connectors: [connector],
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
}

// ── NotebookShell — white card on dark canvas, like hex.tech ──────────────────

function NotebookShell({ children, title = 'Cluster analysis' }: { children: React.ReactNode; title?: string }) {
  return (
    <div style={{
      background: '#111',
      minHeight: '100vh',
      padding: '48px 24px',
      display: 'flex',
      justifyContent: 'center',
    }}>
      <div style={{
        background: '#fff',
        borderRadius: 4,
        width: '100%',
        maxWidth: 860,
        overflow: 'hidden',
        boxShadow: '0 2px 40px rgba(0,0,0,0.5)',
      }}>
        {/* Notebook header */}
        <div style={{
          padding: '28px 32px 20px',
          borderBottom: '1px solid #e8e8e8',
        }}>
          <div style={{ fontSize: 10, fontFamily: 'var(--font-mono)', color: '#bbb', marginBottom: 8, letterSpacing: '0.08em' }}>
            NOTEBOOK
          </div>
          <h1 style={{ fontSize: 26, fontWeight: 700, color: '#111', margin: 0, fontFamily: 'var(--font-sans)' }}>
            {title}
          </h1>
        </div>
        {/* Cells */}
        <div>{children}</div>
      </div>
    </div>
  )
}

// ── Stories ───────────────────────────────────────────────────────────────────

export const Default: Story = {
  name: 'SQL — Idle (hover to reveal actions)',
  args: {
    cell: baseCell,
    ...shared,
    staticSource: sqlSource,
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
    runAt: new Date(Date.now() - 12 * 1000),
  },
}

export const SourceHidden: Story = {
  name: 'SQL — Source Hidden',
  args: {
    ...Default.args,
    sourceVisible: false,
    staticOutput: outputData,
    runAt: new Date(Date.now() - 4 * 60 * 1000),
  },
}

export const MarkdownCell: Story = {
  name: 'Markdown — Text cell',
  args: {
    cell: {
      ...baseCell,
      id: 'cell-md',
      type: 'text',
      language: 'markdown',
      title: 'Context',
      connector_id: undefined,
    },
    staticSource: `In the world of online and in-person shopping, people exhibit patterned behavior depending
on their goals and needs. If enough customers shop at our store, there will eventually be
enough data to create groups of customers where customers exhibit similar behavior.

Using the K-means clustering algorithm and dimensionality reduction, you'll see how to
apply it to customer data in order to find groups of users.`,
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
    cell: { ...baseCell, cell_collapsed: true },
    onToggleCellCollapsed: () => {},
  },
}

export const SaveError: Story = {
  name: 'SQL — Save Error',
  args: {
    ...Default.args,
    saveState: { saving: false, savedAt: null, error: 'connection timeout' },
  },
}

export const HexNotebook: Story = {
  name: '★ Full Notebook — Dark canvas (Hex-style)',
  decorators: [
    () => (
      <NotebookShell title="Cluster analysis">
        {/* Context section */}
        <CellVariantWireframe
          cell={{
            ...baseCell,
            id: 'c-ctx',
            type: 'text',
            language: 'markdown',
            title: 'Context',
            connector_id: undefined,
          }}
          staticSource={`In the world of online and in-person shopping, people exhibit patterned behavior depending on their goals and needs. If enough customers shop at our store, there will eventually be enough data to create groups of customers where customers exhibit similar behavior. However, finding these patterns in the raw data is a daunting task for a human.

Using the K-means clustering algorithm and dimensionality reduction, you'll see how to apply it to customer data in order to find groups of users.`}
          saveState={savedState}
          onDelete={() => {}} onMoveUp={() => {}} onMoveDown={() => {}}
          onSwitchType={() => {}} onToggleSourceVisible={() => {}} onToggleCellCollapsed={() => {}} onShowHistory={() => {}} onUpdateCellMeta={() => {}}
        />

        {/* Data section label */}
        <div style={{ padding: '16px 16px 0', fontSize: 9, fontFamily: 'var(--font-mono)', color: '#bbb', letterSpacing: '0.1em', textTransform: 'uppercase' }}>
          Data
        </div>

        {/* SQL query */}
        <CellVariantWireframe
          cell={{ ...baseCell, id: 'c-sql', title: undefined, connector_id: 'conn-1' }}
          connectors={[connector]}
          staticSource={clusterSource}
          staticOutput={clusterOutput}
          saveState={savedState}
          runAt={new Date(Date.now() - 30 * 1000)}
          onRun={() => {}} onDelete={() => {}} onMoveUp={() => {}} onMoveDown={() => {}}
          onSwitchType={() => {}} onToggleSourceVisible={() => {}} onToggleCellCollapsed={() => {}} onShowHistory={() => {}} onUpdateCellMeta={() => {}}
        />

        {/* Revenue query */}
        <CellVariantWireframe
          cell={{ ...baseCell, id: 'c-rev', title: 'Revenue by account', connector_id: 'conn-1' }}
          connectors={[connector]}
          staticSource={sqlSource}
          staticOutput={outputData}
          saveState={{ saving: false, savedAt: new Date(Date.now() - 5 * 60 * 1000), error: null }}
          runAt={new Date(Date.now() - 5 * 60 * 1000)}
          onRun={() => {}} onDelete={() => {}} onMoveUp={() => {}} onMoveDown={() => {}}
          onSwitchType={() => {}} onToggleSourceVisible={() => {}} onToggleCellCollapsed={() => {}} onShowHistory={() => {}} onUpdateCellMeta={() => {}}
        />

        {/* Collapsed archived cell */}
        <CellVariantWireframe
          cell={{ ...baseCell, id: 'c-arch', cell_collapsed: true, title: 'MoM growth (archived)' }}
          onToggleCellCollapsed={() => {}}
        />
      </NotebookShell>
    ),
  ],
  render: () => <></>,
}
