import { describe, test, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { FormModal } from './FormModal'

describe('FormModal', () => {
  test('renders the shared form shell and submits', () => {
    const onSubmit = vi.fn()
    const onClose = vi.fn()
    render(
      <FormModal title="New Thing" onClose={onClose} submitLabel="Create" onSubmit={onSubmit}>
        <label>Name<input /></label>
      </FormModal>,
    )
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAccessibleName('New Thing')
    expect(dialog.querySelector('.form-modal-grid')).not.toBeNull()
    expect(dialog.querySelector('.form-modal-footer')).not.toBeNull()

    fireEvent.click(screen.getByText('Create'))
    expect(onSubmit).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByText('Cancel'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  test('disables submit and shows the pending label while saving', () => {
    render(
      <FormModal title="Edit Thing" onClose={() => {}} submitLabel="Save" pendingLabel="Saving…" pending onSubmit={() => {}}>
        <span>fields</span>
      </FormModal>,
    )
    const submit = screen.getByText('Saving…')
    expect(submit).toBeDisabled()
  })

  test('surfaces the error message in the footer', () => {
    render(
      <FormModal title="New Thing" onClose={() => {}} error="Name is required" submitLabel="Create" onSubmit={() => {}}>
        <span>fields</span>
      </FormModal>,
    )
    expect(screen.getByText('Name is required')).toBeInTheDocument()
  })
})
