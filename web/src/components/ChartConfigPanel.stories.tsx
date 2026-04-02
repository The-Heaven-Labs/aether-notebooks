import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { ChartConfigPanel } from './ChartConfigPanel'
import type { ChartConfig } from './ChartConfigPanel'

const meta: Meta<typeof ChartConfigPanel> = {
  component: ChartConfigPanel,
  title: 'Components/ChartConfigPanel',
}
export default meta
type Story = StoryObj<typeof ChartConfigPanel>

function InteractiveWrapper() {
  const [config, setConfig] = useState<ChartConfig>({
    chartType: 'bar',
    xAxis: 'month',
    yAxis: ['revenue', 'expenses'],
    showLegend: true,
    showGrid: true,
  })
  return (
    <ChartConfigPanel
      config={config}
      columns={['month', 'revenue', 'expenses', 'profit']}
      onChange={setConfig}
    />
  )
}

export const Interactive: Story = {
  render: () => <InteractiveWrapper />,
}

export const LineChartConfig: Story = {
  args: {
    config: {
      chartType: 'line',
      xAxis: 'date',
      yAxis: ['signups'],
      showLegend: false,
      showGrid: true,
    },
    columns: ['date', 'signups', 'conversions'],
    onChange: () => {},
  },
}
