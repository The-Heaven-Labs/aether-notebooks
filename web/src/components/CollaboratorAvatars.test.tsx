import { describe, test, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import type { Awareness } from 'y-protocols/awareness'
import { CollaboratorAvatars } from './CollaboratorAvatars'

function fakeAwareness(states: Array<{ email: string; name: string; color: string }>): Awareness {
  const map = new Map(states.map((user, i) => [i + 1, { user }]))
  return {
    getStates: () => map,
    on: () => {},
    off: () => {},
  } as unknown as Awareness
}

const BOB = { email: 'bob@test.com', name: 'Bob Smith', color: '#3366cc' }

describe('CollaboratorAvatars', () => {
  test('renders collaborators from awareness and skips the current user', () => {
    render(
      <CollaboratorAvatars
        awareness={fakeAwareness([BOB, { email: 'me@test.com', name: 'Me', color: '#000000' }])}
        currentUserEmail="me@test.com"
      />,
    )

    expect(screen.getByTitle('Bob Smith')).toHaveTextContent('BS')
    expect(screen.queryByTitle('Me')).not.toBeInTheDocument()
  })

  test('presence-only hosts get static badges, not dead follow buttons', () => {
    render(<CollaboratorAvatars awareness={fakeAwareness([BOB])} currentUserEmail="me@test.com" />)

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.getByTitle('Bob Smith')).toBeInTheDocument()
  })

  test('clicking an avatar follows when the host wires onFollow', () => {
    const onFollow = vi.fn()
    const onUnfollow = vi.fn()
    render(
      <CollaboratorAvatars
        awareness={fakeAwareness([BOB])}
        currentUserEmail="me@test.com"
        following={null}
        onFollow={onFollow}
        onUnfollow={onUnfollow}
      />,
    )

    fireEvent.click(screen.getByTitle('Bob Smith'))
    expect(onFollow).toHaveBeenCalledWith({ email: 'bob@test.com', name: 'Bob Smith' })
    expect(onUnfollow).not.toHaveBeenCalled()
  })

  test('renders nothing when awareness is absent', () => {
    const { container } = render(
      <CollaboratorAvatars awareness={null} currentUserEmail="me@test.com" />,
    )

    expect(container.querySelectorAll('[title]')).toHaveLength(0)
  })
})
