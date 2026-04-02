import type { Meta, StoryObj } from '@storybook/react-vite'
import { ConnectorSelector } from './ConnectorSelector'

const meta: Meta<typeof ConnectorSelector> = {
  component: ConnectorSelector,
  title: 'Components/ConnectorSelector',
  parameters: {
    // ConnectorSelector fetches from /api/v1/connectors — will show empty without API
    docs: {
      description: {
        component: 'Dropdown for selecting a database connector. Fetches connector list from the API on mount.',
      },
    },
  },
}
export default meta
type Story = StoryObj<typeof ConnectorSelector>

export const Default: Story = {
  args: {
    value: null,
    onChange: () => {},
    placeholder: 'Select connector',
  },
}

export const WithClear: Story = {
  args: {
    value: null,
    onChange: () => {},
    allowClear: true,
    placeholder: 'Select connector',
  },
}
