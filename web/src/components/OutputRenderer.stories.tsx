import type { Meta, StoryObj } from '@storybook/react-vite'
import { OutputRenderer } from './OutputRenderer'

const meta: Meta<typeof OutputRenderer> = {
  component: OutputRenderer,
  title: 'Components/OutputRenderer',
}
export default meta
type Story = StoryObj<typeof OutputRenderer>

export const TableOutput: Story = {
  args: {
    outputs: [{
      type: 'table',
      data: {
        columns: [
          { name: 'id', type: 'int4' },
          { name: 'name', type: 'text' },
          { name: 'active', type: 'bool' },
          { name: 'score', type: 'float8' },
        ],
        rows: [
          [1, 'Alice', true, 98.5],
          [2, 'Bob', false, 72.1],
          [3, 'Carol', true, 85.0],
        ],
      },
    }],
  },
}

export const UUIDAndJsonColumns: Story = {
  args: {
    outputs: [{
      type: 'table',
      data: {
        columns: [
          { name: 'id', type: 'uuid' },
          { name: 'metadata', type: 'jsonb' },
          { name: 'created_at', type: 'timestamp' },
        ],
        rows: [
          [
            '550e8400-e29b-41d4-a716-446655440000',
            { env: 'prod', version: 3 },
            '2024-01-15T10:30:00Z',
          ],
          [
            'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11',
            { env: 'staging', version: 1 },
            '2024-01-14T08:00:00Z',
          ],
        ],
      },
    }],
  },
}

export const NullValues: Story = {
  args: {
    outputs: [{
      type: 'table',
      data: {
        columns: [
          { name: 'id', type: 'int4' },
          { name: 'optional', type: 'text' },
          { name: 'value', type: 'float8' },
        ],
        rows: [
          [1, null, 3.14],
          [2, 'present', null],
          [3, null, null],
        ],
      },
    }],
  },
}

export const ChartOutput: Story = {
  args: {
    outputs: [{
      type: 'table',
      data: {
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
        ],
      },
    }],
  },
}

export const ErrorOutput: Story = {
  args: {
    outputs: [{
      type: 'error',
      data: 'ERROR: relation "missing_table" does not exist\nLINE 1: SELECT * FROM missing_table\n                      ^',
    }],
  },
}

export const TextOutput: Story = {
  args: {
    outputs: [{
      type: 'text',
      data: 'Query executed successfully. 42 rows affected.',
    }],
  },
}

export const FixedChartView: Story = {
  args: {
    fixedView: 'chart',
    outputs: [{
      type: 'table',
      data: {
        columns: [
          { name: 'month', type: 'text' },
          { name: 'signups', type: 'int4' },
        ],
        rows: [
          ['Jan', 120],
          ['Feb', 145],
          ['Mar', 98],
          ['Apr', 210],
          ['May', 187],
        ],
      },
    }],
  },
}
