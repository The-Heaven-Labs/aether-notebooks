import type { Meta, StoryObj } from '@storybook/react-vite'
import { FormModal } from './FormModal'

const meta: Meta<typeof FormModal> = {
  title: 'Components/FormModal',
  component: FormModal,
}

export default meta
type Story = StoryObj<typeof FormModal>

const fieldStyle: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 4,
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--text-secondary)',
}

const inputStyle: React.CSSProperties = {
  padding: '6px 10px',
  border: '1px solid var(--border)',
  borderRadius: 4,
  fontSize: 13,
  fontFamily: 'var(--font-mono)',
  background: 'var(--bg-input)',
  color: 'var(--text-primary)',
}

export const Create: Story = {
  args: {
    title: 'New Connector',
    onClose: () => {},
    submitLabel: 'Create',
    onSubmit: () => {},
    children: (
      <>
        <label style={fieldStyle}>Name<input style={inputStyle} defaultValue="My Postgres" /></label>
        <label style={fieldStyle}>Host<input style={inputStyle} defaultValue="localhost" /></label>
      </>
    ),
  },
}

export const PendingWithError: Story = {
  args: {
    title: 'Edit "Staging DB"',
    onClose: () => {},
    error: 'Host is required',
    submitLabel: 'Save',
    pendingLabel: 'Saving…',
    pending: true,
    onSubmit: () => {},
    children: (
      <label style={fieldStyle}>Name<input style={inputStyle} defaultValue="Staging DB" /></label>
    ),
  },
}
