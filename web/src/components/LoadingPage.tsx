import { LoadingSpinner } from './LoadingSpinner'

interface Props {
  message?: string
}

export function LoadingPage({ message }: Props) {
  return (
    <div style={styles.page}>
      {message ? (
        <p style={styles.text}>{message}</p>
      ) : (
        <LoadingSpinner />
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: '100vh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'var(--bg-primary)',
  },
  text: {
    fontSize: 14,
    color: 'var(--text-secondary)',
  },
}