import type { Meta, StoryObj } from '@storybook/react-vite'
import { Pencil, ShieldCheck, Star, Trash2, Zap } from 'lucide-react'
import { RowAction, RowActionsBreak, RowActionsCell, RowActionsHeader } from './RowActions'
import { StyledTable, rowStyle, cellStyle } from './StyledTable'

const meta: Meta<typeof RowAction> = {
  title: 'Components/RowActions',
  component: RowAction,
}

export default meta
type Story = StoryObj<typeof RowAction>

export const Default: Story = {
  args: {
    label: 'Edit connector',
    icon: <Pencil size={13} />,
    accent: true,
    onClick: () => {},
  },
}

export const Variants: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 6 }}>
      <RowAction label="Test connection" icon={<Zap size={13} />} onClick={() => {}} />
      <RowAction label="Edit connector" icon={<Pencil size={13} />} accent onClick={() => {}} />
      <RowAction label="Permissions" icon={<ShieldCheck size={13} />} onClick={() => {}} />
      <RowAction label="Set as default" icon={<Star size={13} />} onClick={() => {}} />
      <RowAction label="Delete connector" icon={<Trash2 size={13} />} danger onClick={() => {}} />
    </div>
  ),
}

export const InTable: Story = {
  render: () => (
    <StyledTable
      headers={['Name', 'Type', 'Status', <RowActionsHeader key="actions" />]}
      headerClassNames={[undefined, undefined, undefined, 'row-actions-header']}
    >
      <tr style={rowStyle}>
        <td style={cellStyle}>Production DB</td>
        <td style={cellStyle}>postgres</td>
        <td style={cellStyle}>Connected</td>
        <RowActionsCell>
          <RowAction label="Test connection" icon={<Zap size={13} />} onClick={() => {}} />
          <RowAction label="Edit connector" icon={<Pencil size={13} />} accent onClick={() => {}} />
          <RowActionsBreak />
          <RowAction label="Permissions" icon={<ShieldCheck size={13} />} onClick={() => {}} />
          <RowAction label="Delete connector" icon={<Trash2 size={13} />} danger onClick={() => {}} />
        </RowActionsCell>
      </tr>
      <tr style={rowStyle}>
        <td style={cellStyle}>Analytics</td>
        <td style={cellStyle}>clickhouse</td>
        <td style={cellStyle}>Unknown — click Test</td>
        <RowActionsCell>
          <RowAction label="Test connection" icon={<Zap size={13} />} spinning disabled onClick={() => {}} />
          <RowAction label="Edit connector" icon={<Pencil size={13} />} accent onClick={() => {}} />
          <RowActionsBreak />
          <RowAction label="Permissions" icon={<ShieldCheck size={13} />} onClick={() => {}} />
          <RowAction label="Delete connector" icon={<Trash2 size={13} />} danger onClick={() => {}} />
        </RowActionsCell>
      </tr>
    </StyledTable>
  ),
}
