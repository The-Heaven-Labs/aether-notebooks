import type { Meta, StoryObj } from '@storybook/react-vite'
import { ChartView } from './index'

const meta: Meta<typeof ChartView> = {
  component: ChartView,
  title: 'Charts/ChartView',
}
export default meta
type Story = StoryObj<typeof ChartView>

const monthlyData = {
  columns: [
    { name: 'month', type: 'text' },
    { name: 'revenue', type: 'float8' },
    { name: 'expenses', type: 'float8' },
  ],
  rows: [
    ['Jan', 12000, 9000], ['Feb', 15000, 10500], ['Mar', 11000, 8000],
    ['Apr', 18000, 12000], ['May', 21000, 14000], ['Jun', 19000, 13500],
  ],
}

const categoryData = {
  columns: [
    { name: 'category', type: 'text' },
    { name: 'count', type: 'int4' },
  ],
  rows: [
    ['Engineering', 42], ['Sales', 28], ['Marketing', 19], ['Support', 15], ['Design', 11],
  ],
}

const timelineData = {
  columns: [
    { name: 'timestamp', type: 'timestamp' },
    { name: 'event', type: 'text' },
    { name: 'level', type: 'text' },
  ],
  rows: [
    ['2024-01-15T08:00:00', 'System start', 'info'],
    ['2024-01-15T08:15:00', 'User login', 'info'],
    ['2024-01-15T08:30:00', 'Failed auth', 'warn'],
    ['2024-01-15T09:00:00', 'Query executed', 'info'],
    ['2024-01-15T09:45:00', 'Timeout error', 'error'],
    ['2024-01-15T10:00:00', 'Retry success', 'info'],
  ],
}

const rangeTimelineData = {
  columns: [
    { name: 'task', type: 'text' },
    { name: 'start', type: 'timestamp' },
    { name: 'end', type: 'timestamp' },
  ],
  rows: [
    ['Build', '2024-01-15T09:00:00', '2024-01-15T09:30:00'],
    ['Test', '2024-01-15T09:30:00', '2024-01-15T10:00:00'],
    ['Deploy', '2024-01-15T10:00:00', '2024-01-15T10:15:00'],
  ],
}

const treeData = {
  columns: [
    { name: 'pid', type: 'int4' },
    { name: 'ppid', type: 'int4' },
    { name: 'name', type: 'text' },
    { name: 'cpu', type: 'float' },
  ],
  rows: [
    [1, 0, 'systemd', 0.1],
    [2, 1, 'sshd', 0.5],
    [3, 1, 'nginx', 1.2],
    [4, 2, 'sshd-session', 0.3],
    [5, 3, 'worker', 2.1],
    [6, 3, 'worker', 1.8],
  ],
}

export const Bar: Story = { args: { rs: monthlyData } }
export const StackedBar: Story = { args: { output: { type: 'table', data: monthlyData, config: { chartType: 'bar', barMode: 'stacked', xAxis: 'month', yAxis: ['revenue', 'expenses'] } } } }
export const Line: Story = { args: { output: { type: 'table', data: monthlyData, config: { chartType: 'line', xAxis: 'month', yAxis: ['revenue', 'expenses'] } } } }
export const Area: Story = { args: { output: { type: 'table', data: monthlyData, config: { chartType: 'area', xAxis: 'month', yAxis: ['revenue', 'expenses'] } } } }
export const Scatter: Story = { args: { output: { type: 'table', data: monthlyData, config: { chartType: 'scatter', xAxis: 'month', yAxis: ['revenue'] } } } }
export const Pie: Story = { args: { output: { type: 'table', data: categoryData, config: { chartType: 'pie', xAxis: 'category', yAxis: ['count'] } } } }
export const Donut: Story = { args: { output: { type: 'table', data: categoryData, config: { chartType: 'donut', xAxis: 'category', yAxis: ['count'] } } } }
export const TimelineEvents: Story = { args: { output: { type: 'table', data: timelineData, config: { chartType: 'timeline', timeColumn: 'timestamp', labelColumn: 'event', groupBy: 'level' } } } }
export const TimelineRanges: Story = { args: { output: { type: 'table', data: rangeTimelineData, config: { chartType: 'timeline', timeColumn: 'start', endTimeColumn: 'end', labelColumn: 'task' } } } }
export const HierarchyTree: Story = { args: { output: { type: 'table', data: treeData, config: { chartType: 'hierarchy_tree', idColumn: 'pid', parentIdColumn: 'ppid', labelColumn: 'name', metricColumns: ['cpu'], layout: 'top-down' } } } }
