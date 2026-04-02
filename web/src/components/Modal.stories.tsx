import type { Meta, StoryObj } from '@storybook/react-vite'
import { Modal } from './Modal'

const meta: Meta<typeof Modal> = {
  component: Modal,
}

export default meta
type Story = StoryObj<typeof Modal>

export const Default: Story = {
  args: {
    title: 'Keyboard Shortcuts',
    onClose: () => {},
    children: <p style={{ padding: 20 }}>Modal content goes here.</p>,
  },
}

export const NarrowModal: Story = {
  args: {
    title: 'Confirm',
    minWidth: 320,
    onClose: () => {},
    children: <p style={{ padding: 20 }}>Are you sure?</p>,
  },
}
