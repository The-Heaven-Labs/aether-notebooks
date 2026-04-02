import type { Meta, StoryObj } from '@storybook/react-vite'
import { StyledTable, rowStyle, cellStyle } from './StyledTable'

const meta: Meta<typeof StyledTable> = {
  title: 'Components/StyledTable',
  component: StyledTable,
}

export default meta
type Story = StoryObj<typeof StyledTable>

export const Default: Story = {
  args: {
    headers: ['Name', 'Type', 'Host', 'Status'],
    children: (
      <>
        <tr style={rowStyle}>
          <td style={cellStyle}>Production DB</td>
          <td style={cellStyle}>postgres</td>
          <td style={cellStyle}>db.example.com</td>
          <td style={cellStyle}>Connected</td>
        </tr>
        <tr style={rowStyle}>
          <td style={cellStyle}>Analytics</td>
          <td style={cellStyle}>clickhouse</td>
          <td style={cellStyle}>ch.example.com</td>
          <td style={cellStyle}>Connected</td>
        </tr>
        <tr style={rowStyle}>
          <td style={cellStyle}>Staging DB</td>
          <td style={cellStyle}>postgres</td>
          <td style={cellStyle}>staging.example.com</td>
          <td style={cellStyle}>—</td>
        </tr>
      </>
    ),
  },
}

export const Empty: Story = {
  args: {
    headers: ['Name', 'Type'],
    children: (
      <tr>
        <td colSpan={2} style={{ padding: '40px', textAlign: 'center', color: 'var(--text-muted)' }}>
          No items yet.
        </td>
      </tr>
    ),
  },
}

export const AuditVariant: Story = {
  args: {
    headers: ['Timestamp', 'Action', 'User'],
    thStyle: { fontSize: 12, background: 'var(--bg-primary)' },
    children: (
      <>
        <tr style={rowStyle}>
          <td style={cellStyle}>Apr 1, 2026 10:30:00</td>
          <td style={cellStyle}>notebook.create</td>
          <td style={cellStyle}>alice@example.com</td>
        </tr>
        <tr style={rowStyle}>
          <td style={cellStyle}>Apr 1, 2026 10:25:00</td>
          <td style={cellStyle}>connector.delete</td>
          <td style={cellStyle}>bob@example.com</td>
        </tr>
        <tr style={rowStyle}>
          <td style={cellStyle}>Apr 1, 2026 10:20:00</td>
          <td style={cellStyle}>member.invite</td>
          <td style={cellStyle}>alice@example.com</td>
        </tr>
      </>
    ),
  },
}
