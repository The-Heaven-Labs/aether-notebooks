import type { Meta, StoryObj } from '@storybook/react-vite'
import { ChartView } from './ChartView'

const meta: Meta<typeof ChartView> = {
  component: ChartView,
  title: 'Components/ChartView',
  parameters: {
    a11y: {
      config: {
        rules: [{ id: 'button-name', enabled: false }],
      },
    },
  },
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
    ['Jan', 12000, 9000],
    ['Feb', 15000, 10500],
    ['Mar', 11000, 8000],
    ['Apr', 18000, 12000],
    ['May', 21000, 14000],
    ['Jun', 19000, 13500],
  ],
}

const categoryData = {
  columns: [
    { name: 'category', type: 'text' },
    { name: 'count', type: 'int4' },
  ],
  rows: [
    ['Engineering', 42],
    ['Sales', 28],
    ['Marketing', 19],
    ['Support', 15],
    ['Design', 11],
  ],
}

export const BarChart: Story = {
  args: {
    rs: { columns: monthlyData.columns, rows: monthlyData.rows },
  },
}

export const StackedBar: Story = {
  args: {
    output: {
      type: 'table',
      data: monthlyData,
      config: { chartType: 'stacked_bar', xAxis: 'month', yAxis: ['revenue', 'expenses'] },
    },
  },
}

export const LineChart: Story = {
  args: {
    output: {
      type: 'table',
      data: monthlyData,
      config: { chartType: 'line', xAxis: 'month', yAxis: ['revenue', 'expenses'] },
    },
  },
}

export const AreaChart: Story = {
  args: {
    output: {
      type: 'table',
      data: monthlyData,
      config: { chartType: 'area', xAxis: 'month', yAxis: ['revenue', 'expenses'] },
    },
  },
}

export const PieChart: Story = {
  args: {
    output: {
      type: 'table',
      data: categoryData,
      config: { chartType: 'pie', xAxis: 'category', yAxis: ['count'] },
    },
  },
}

export const DonutChart: Story = {
  args: {
    output: {
      type: 'table',
      data: categoryData,
      config: { chartType: 'donut', xAxis: 'category', yAxis: ['count'] },
    },
  },
}

export const ScatterPlot: Story = {
  args: {
    output: {
      type: 'table',
      data: {
        columns: [
          { name: 'x', type: 'float8' },
          { name: 'y', type: 'float8' },
        ],
        rows: [
          [1.2, 3.4], [2.8, 5.1], [4.0, 2.9], [5.5, 7.2],
          [3.3, 4.8], [6.1, 6.0], [7.4, 8.3], [2.1, 1.5],
        ],
      },
      config: { chartType: 'scatter', xAxis: 'x', yAxis: ['y'] },
    },
  },
}

export const SingleDataPoint: Story = {
  name: 'Line: Single Data Point (dot visible)',
  args: {
    output: {
      type: 'table',
      data: {
        columns: [{ name: 'date', type: 'text' }, { name: 'value', type: 'int4' }],
        rows: [['2024-01', 42]],
      },
      config: { chartType: 'line', xAxis: 'date', yAxis: ['value'] },
    },
  },
}
