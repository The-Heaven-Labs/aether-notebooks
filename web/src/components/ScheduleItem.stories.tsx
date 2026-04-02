import type { Meta, StoryObj } from '@storybook/react-vite'
import { ScheduleItem } from './ScheduleItem'

const meta: Meta<typeof ScheduleItem> = {
  component: ScheduleItem,
}

export default meta
type Story = StoryObj<typeof ScheduleItem>

const baseSchedule = {
  id: 'sched-1',
  notebook_id: 'nb-1',
  cron_expression: '0 9 * * 1-5',
  next_run_at: '2024-02-01T09:00:00Z',
  created_at: '2024-01-15T00:00:00Z',
  updated_at: '2024-01-15T00:00:00Z',
  parameter_overrides: {},
}

export const Enabled: Story = {
  args: {
    schedule: { ...baseSchedule, enabled: true },
    onToggle: () => {},
    onDelete: () => {},
  },
}

export const Disabled: Story = {
  args: {
    schedule: { ...baseSchedule, enabled: false },
    onToggle: () => {},
    onDelete: () => {},
  },
}

export const WithError: Story = {
  args: {
    schedule: { ...baseSchedule, enabled: true },
    onToggle: () => {},
    onDelete: () => {},
    error: 'Failed to toggle schedule',
  },
}
