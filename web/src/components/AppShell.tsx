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
        <main style={{ ...styles.main, background: 'var(--bg-primary)', ...(noPadding ? { padding: 0, overflow: 'hidden', display: 'flex', flexDirection: 'column' } : {}) }}>{children}</main>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  root: { display: 'flex', flexDirection: 'column', height: '100vh', maxHeight: '100vh', overflow: 'hidden', background: 'var(--bg-primary)' },
  body: { display: 'flex', flex: 1, overflow: 'hidden', minHeight: 0 },
  main: { flex: 1, overflow: 'auto', padding: '32px', minHeight: 0 },
}
