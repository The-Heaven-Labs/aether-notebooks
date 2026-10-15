import { describe, test, expect, vi } from 'vitest'
import { useState } from 'react'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { ConfirmModal } from './ConfirmModal'
import { SearchPalette } from './SearchPalette'
import { renderWithProviders } from '../test/utils'

function StackedHarness() {
  const [modalOpen, setModalOpen] = useState(true)
  const [paletteOpen, setPaletteOpen] = useState(false)
  return (
    <>
      {modalOpen && (
        <ConfirmModal
          title="Discard changes"
          message="You have unsaved changes. Discard them?"
          onConfirm={() => {}}
          onCancel={() => setModalOpen(false)}
        />
      )}
      <button type="button" onClick={() => setPaletteOpen(true)}>Open search</button>
      {paletteOpen && <SearchPalette open onClose={() => setPaletteOpen(false)} />}
    </>
  )
}

describe('ConfirmModal Escape handling', () => {
  test('Escape closes the modal when it is the only layer', async () => {
    const onCancel = vi.fn()
    renderWithProviders(
      <ConfirmModal
        title="Discard changes"
        message="You have unsaved changes. Discard them?"
        onConfirm={() => {}}
        onCancel={onCancel}
      />,
    )

    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  test('Escape closes only the palette while it sits above the modal, then the modal', async () => {
    renderWithProviders(<StackedHarness />)

    expect(screen.getByText('Discard changes')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Open search' }))
    expect(await screen.findByRole('dialog', { name: 'Search palette' })).toBeInTheDocument()

    fireEvent.keyDown(document.body, { key: 'Escape' })
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Search palette' })).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Discard changes')).toBeInTheDocument()

    fireEvent.keyDown(document.body, { key: 'Escape' })
    await waitFor(() =>
      expect(screen.queryByText('Discard changes')).not.toBeInTheDocument(),
    )
  })
})
