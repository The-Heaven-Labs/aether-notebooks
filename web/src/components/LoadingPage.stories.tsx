import type { Meta, StoryObj } from '@storybook/react-vite'
import { LoadingPage } from './LoadingPage'

const meta: Meta<typeof LoadingPage> = {
  component: LoadingPage,
}

export default meta
type Story = StoryObj<typeof LoadingPage>

export const Default: Story = {
  args: {},
}

export const WithMessage: Story = {
  args: {
    message: 'Loading notebook...',
  },
}

export const NotFound: Story = {
  args: {
    message: 'Notebook not found',
  },
}