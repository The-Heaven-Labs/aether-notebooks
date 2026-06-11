import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'

interface Command {
  command: string
  description: string
}

interface Skill {
  id: string
  name: string
  description?: string
}

const COMMANDS: Command[] = [
  { command: '/new', description: 'Start a fresh session' },
  { command: '/skills', description: 'List available skills' },
  { command: '/agents', description: 'List available agents' },
  { command: '/summarize', description: 'Summarize the current session' },
]

interface Props {
  filter: string
  onSelect: (command: string) => void
  onClose: () => void
}

export function SlashCommandPicker({ filter, onSelect, onClose }: Props) {
  const [selectedIndex, setSelectedIndex] = useState(0)

  // Fetch skills for autocomplete
  const { data: skills = [] } = useQuery<Skill[]>({
    queryKey: ['skills'],
    queryFn: () => api.get<Skill[]>('/api/v1/skills'),
    staleTime: 5 * 60 * 1000,
  })

  // Check if user is typing a skill command: /skill: or /skill:something
  const skillMatch = filter.match(/^\/skill:?(.*)$/i)
  const isSkillAutocomplete = skillMatch !== null
  const skillSearchTerm = skillMatch ? skillMatch[1].toLowerCase() : ''

  // Build filtered list based on context
  let filtered: Command[]
  if (isSkillAutocomplete) {
    // Show skills that match the search term
    const matchedSkills = skills.filter((s) =>
      s.name.toLowerCase().includes(skillSearchTerm)
    )
    filtered = matchedSkills.map((s) => ({
      command: `/skill:${s.name.toLowerCase().replace(/\s+/g, '-')}`,
      description: s.description || s.name,
    }))
  } else {
    // Show regular commands filtered by the input
    filtered = COMMANDS.filter((c) =>
      c.command.toLowerCase().includes(filter.toLowerCase())
    )
  }

  useEffect(() => {
    setSelectedIndex(0)
  }, [filter])

  useEffect(() => {
    if (filtered.length === 0) return

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        e.stopPropagation()
        setSelectedIndex((i) => Math.min(i + 1, filtered.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        e.stopPropagation()
        setSelectedIndex((i) => Math.max(i - 1, 0))
      } else if (e.key === 'Enter') {
        e.preventDefault()
        e.stopPropagation()
        if (filtered[selectedIndex]) {
          onSelect(filtered[selectedIndex].command)
        }
      } else if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        onClose()
      }
    }

    window.addEventListener('keydown', handleKeyDown, true)
    return () => window.removeEventListener('keydown', handleKeyDown, true)
  }, [filter, filtered, selectedIndex, onSelect, onClose])

  if (filtered.length === 0) return null

  return (
    <div style={styles.picker}>
      {filtered.map((cmd, i) => (
        <button
          key={cmd.command}
          style={{
            ...styles.item,
            ...(i === selectedIndex ? styles.selectedItem : {}),
          }}
          onClick={() => onSelect(cmd.command)}
          onMouseEnter={() => setSelectedIndex(i)}
        >
          <span style={styles.cmdName}>{cmd.command}</span>
          <span style={styles.cmdDesc}>{cmd.description}</span>
        </button>
      ))}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  picker: {
    position: 'absolute',
    bottom: '100%',
    left: 12,
    right: 12,
    marginBottom: 4,
    background: 'var(--bg-primary)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    boxShadow: '0 4px 16px rgba(0,0,0,0.15)',
    overflow: 'hidden',
    zIndex: 200,
  },
  item: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    width: '100%',
    padding: '8px 12px',
    border: 'none',
    background: 'none',
    cursor: 'pointer',
    textAlign: 'left' as const,
    fontSize: 13,
  },
  selectedItem: {
    background: 'var(--bg-secondary)',
  },
  cmdName: {
    fontWeight: 600,
    color: 'var(--accent)',
    minWidth: 80,
  },
  cmdDesc: {
    color: 'var(--text-muted)',
    fontSize: 12,
  },
}
