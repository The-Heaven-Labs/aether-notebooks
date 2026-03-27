import { TopBar } from './TopBar'
import { Sidebar } from './Sidebar'

interface Props {
  children: React.ReactNode
  noPadding?: boolean
}

export function AppShell({ children, noPadding }: Props) {
  return (
    <div style={styles.root}>
      <TopBar />
      <div style={styles.body}>
        <Sidebar />
        <main style={{ ...styles.main, ...(noPadding ? { padding: 0, overflow: 'hidden' } : {}) }}>{children}</main>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  root: { display: 'flex', flexDirection: 'column', minHeight: '100vh', background: 'var(--bg-primary)' },
  body: { display: 'flex', flex: 1, overflow: 'hidden' },
  main: { flex: 1, overflow: 'auto', padding: '32px' },
}
