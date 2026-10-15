import { describe, test, expect, vi } from 'vitest'
import { useState } from 'react'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { ConfirmDialog } from './ConfirmDialog'
import { SearchPalette } from './SearchPalette'
import { renderWithProviders } from '../test/utils'

/**
 * The reported double-close: a dialog mounted before the palette used to own
 * its own document `keydown` listener, which ran in registration order —
 * before the palette's shared-stack handler — and closed the dialog on the
 * same Escape that closed the palette. The dialog must instead participate in
 * the shared stack and stay open until the palette is gone.
 */
function StackedHarness({ onConfirm = () => {} }: { onConfirm?: () => void }) {
  const [dialogOpen, setDialogOpen] = useState(true)
  const [paletteOpen, setPaletteOpen] = useState(false)
  return (
    <>
      <ConfirmDialog
        open={dialogOpen}
        title="Delete item"
        message="Delete this item?"
        confirmLabel="Delete"
        destructive
        onConfirm={onConfirm}
        onCancel={() => setDialogOpen(false)}
      />
      <button type="button" onClick={() => setPaletteOpen(true)}>Open search</button>
      {paletteOpen && <SearchPalette open onClose={() => setPaletteOpen(false)} />}
    </>
  )
}

describe('ConfirmDialog Escape handling', () => {
  test('Escape closes the dialog when it is the only layer', async () => {
    const onCancel = vi.fn()
    renderWithProviders(
      <ConfirmDialog
        open
        title="Delete item"
        message="Delete this item?"
        confirmLabel="Delete"
        onConfirm={() => {}}
        onCancel={onCancel}
      />,
    )

    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  test('Escape closes only the palette while it sits above the dialog, then the dialog', async () => {
    renderWithProviders(<StackedHarness />)

    expect(screen.getByRole('dialog', { name: 'Delete item' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Open search' }))
    expect(await screen.findByRole('dialog', { name: 'Search palette' })).toBeInTheDocument()

    // First Escape belongs to the topmost layer: the palette.
    fireEvent.keyDown(document.body, { key: 'Escape' })
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Search palette' })).not.toBeInTheDocument(),
    )
    expect(screen.getByRole('dialog', { name: 'Delete item' })).toBeInTheDocument()

    // The next Escape now reaches the dialog underneath.
    fireEvent.keyDown(document.body, { key: 'Escape' })
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Delete item' })).not.toBeInTheDocument(),
    )
  })

  test('clicking Cancel still closes the dialog', async () => {
    renderWithProviders(<StackedHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Delete item' })).not.toBeInTheDocument(),
    )
  })
})
