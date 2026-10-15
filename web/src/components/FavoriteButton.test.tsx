import { describe, test, expect, vi } from 'vitest'
import { screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { favoritesFixture } from '../test/handlers'
import { renderWithProviders } from '../test/utils'
import { FavoriteButton } from './FavoriteButton'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

describe('FavoriteButton', () => {
  test('renders an inactive bookmark with the add wording', async () => {
    server.use(...favoritesFixture().handlers)
    renderWithProviders(<FavoriteButton resourceType="notebook" resourceId="nb-1" />)

    const button = await screen.findByRole('button', { name: 'Add to favorites' })
    expect(button).toHaveAttribute('aria-pressed', 'false')
    expect(button).toHaveAttribute('title', 'Add to favorites')
    expect(button.className).toContain('row-action')
  })

  test('renders active when the resource is already favorited', async () => {
    server.use(...favoritesFixture([{ type: 'notebook', id: 'nb-1' }]).handlers)
    renderWithProviders(<FavoriteButton resourceType="notebook" resourceId="nb-1" />)

    const button = await screen.findByRole('button', { name: 'Remove from favorites' })
    expect(button).toHaveAttribute('aria-pressed', 'true')
  })

  test('flips on click and back on a second click', async () => {
    server.use(...favoritesFixture().handlers)
    renderWithProviders(<FavoriteButton resourceType="notebook" resourceId="nb-1" />)

    await userEvent.click(await screen.findByRole('button', { name: 'Add to favorites' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true'),
    )

    const active = screen.getByRole('button', { name: 'Remove from favorites' })
    await waitFor(() => expect(active).toBeEnabled())
    await userEvent.click(active)
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Add to favorites' })).toHaveAttribute('aria-pressed', 'false'),
    )
  })

  test('rolls back to inactive when the request fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const put = deferred<void>()
      server.use(...favoritesFixture().handlers)
      server.use(
        http.put('/api/v1/favorites/notebook/nb-1', async () => {
          await put.promise
          return HttpResponse.json({ error: 'boom' }, { status: 500 })
        }),
      )
      renderWithProviders(<FavoriteButton resourceType="notebook" resourceId="nb-1" />)

      await userEvent.click(await screen.findByRole('button', { name: 'Add to favorites' }))
      await waitFor(() =>
        expect(screen.getByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true'),
      )

      await act(async () => {
        put.resolve()
        await put.promise
      })
      await waitFor(() =>
        expect(screen.getByRole('button', { name: 'Add to favorites' })).toHaveAttribute('aria-pressed', 'false'),
      )
      expect(consoleError).toHaveBeenCalledWith('Failed to add favorite:', expect.anything())
    } finally {
      consoleError.mockRestore()
    }
  })

  test('does not bubble the click to the row', async () => {
    const rowClick = vi.fn()
    server.use(...favoritesFixture().handlers)
    renderWithProviders(
      <div onClick={rowClick}>
        <FavoriteButton resourceType="notebook" resourceId="nb-1" />
      </div>,
    )

    await userEvent.click(await screen.findByRole('button', { name: 'Add to favorites' }))
    expect(rowClick).not.toHaveBeenCalled()

    // Sanity: the row itself still receives its own clicks.
    const button = screen.getByRole('button', { name: 'Remove from favorites' })
    const row = button.parentElement
    expect(row).not.toBeNull()
    if (row) await userEvent.click(row)
    expect(rowClick).toHaveBeenCalledTimes(1)
  })

  test('activates with Enter and Space', async () => {
    server.use(...favoritesFixture().handlers)
    renderWithProviders(<FavoriteButton resourceType="notebook" resourceId="nb-1" />)

    const button = await screen.findByRole('button', { name: 'Add to favorites' })
    // The listing must land (the button un-disables) before it can take focus.
    await waitFor(() => expect(button).toBeEnabled())
    button.focus()
    await userEvent.keyboard('{Enter}')
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true'),
    )

    const active = screen.getByRole('button', { name: 'Remove from favorites' })
    await waitFor(() => expect(active).toBeEnabled())
    active.focus()
    await userEvent.keyboard('[Space]')
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Add to favorites' })).toHaveAttribute('aria-pressed', 'false'),
    )
  })

  test('disables itself until the favorites listing has data', async () => {
    let release: () => void = () => {}
    const parked = new Promise<void>((resolve) => { release = resolve })
    server.use(
      http.get('/api/v1/favorites', async () => {
        await parked
        return HttpResponse.json({ groups: [] })
      }),
    )
    renderWithProviders(<FavoriteButton resourceType="notebook" resourceId="nb-1" />)

    // The current state is unknown until the listing lands, so no click can
    // silently PUT an already-favorited resource.
    const button = await screen.findByRole('button', { name: 'Add to favorites' })
    expect(button).toBeDisabled()

    release()
    await waitFor(() => expect(button).toBeEnabled())
  })

  test('stays enabled after a listing error (add-only fallback)', async () => {
    let putCalls = 0
    server.use(
      http.get('/api/v1/favorites', () => HttpResponse.json({ error: 'boom' }, { status: 500 })),
      http.put('/api/v1/favorites/notebook/nb-1', () => {
        putCalls += 1
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithProviders(<FavoriteButton resourceType="notebook" resourceId="nb-1" />)

    const button = await screen.findByRole('button', { name: 'Add to favorites' })
    // A transient listing failure must not leave every bookmark dead; the
    // toggle keeps its documented add-only semantics instead.
    await waitFor(() => expect(button).toBeEnabled())
    await userEvent.click(button)
    await waitFor(() => expect(putCalls).toBe(1))
  })
})
