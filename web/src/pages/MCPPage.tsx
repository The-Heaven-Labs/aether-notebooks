import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { api } from '../api/client'
import type { Agent } from '../types/agent'

interface MCPServerDisplay {
  name: string
  type: string
  command: string
  args?: string[]
  agentId: string
  agentName: string
}

export function MCPPage() {
  useEffect(() => { document.title = "MCP Servers — Heaven's Notebooks" }, [])

  const { data: agents = [], isLoading } = useQuery<Agent[]>({
    queryKey: ['agents'],
    queryFn: () => api.get<Agent[]>('/api/v1/agents'),
  })

  const servers: MCPServerDisplay[] = []
  for (const agent of agents) {
    for (const mcp of agent.mcp_servers ?? []) {
      servers.push({
        name: mcp.name,
        type: mcp.type,
        command: mcp.command,
        args: mcp.args,
        agentId: agent.id,
        agentName: agent.name,
      })
    }
  }

  return (
    <AppShell>
      <div style={styles.body}>
        <SectionHeader title="MCP Servers" subtitle={servers.length > 0 ? `${servers.length} server${servers.length !== 1 ? 's' : ''} across ${agents.filter(a => a.mcp_servers?.length > 0).length} agent${agents.filter(a => a.mcp_servers?.length > 0).length !== 1 ? 's' : ''}` : ''} />

        <div style={styles.info}>
          MCP servers are configured per-agent on the Agents page. Each agent can define multiple MCP servers (stdio or HTTP) that extend its capabilities.
        </div>

        <StyledTable headers={['Server Name', 'Type', 'Command', 'Args', 'Agent']}>
          {servers.map((s, i) => (
            <tr key={i} style={rowStyle}>
              <td style={cellStyle}><strong>{s.name}</strong></td>
              <td style={cellStyle}><code style={styles.badge}>{s.type}</code></td>
              <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{s.command}</td>
              <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--text-secondary)' }}>{s.args?.join(' ') || '—'}</td>
              <td style={cellStyle}>{s.agentName}</td>
            </tr>
          ))}
          {servers.length === 0 && !isLoading && (
            <tr>
              <td colSpan={5} style={{ ...cellStyle, textAlign: 'center', color: 'var(--text-muted)', padding: 40 }}>
                No MCP servers configured yet. Add MCP servers to an agent on the Agents page.
              </td>
            </tr>
          )}
        </StyledTable>
      </div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  info: { fontSize: 13, color: 'var(--text-muted)', marginBottom: 16, background: 'var(--bg-secondary)', padding: '10px 14px', borderRadius: 6, border: '1px solid var(--border)' },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--accent-light)', color: 'var(--text-secondary)', padding: '2px 7px', borderRadius: 3 },
}

type RowProps = React.CSSProperties
const rowStyle: RowProps = { borderBottom: '1px solid var(--border)' }
const cellStyle: React.CSSProperties = { padding: '10px 16px', verticalAlign: 'top' }

function StyledTable({ headers, children }: { headers: string[]; children: React.ReactNode }) {
  return (
    <table style={{ width: '100%', borderCollapse: 'collapse', border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
      <thead>
        <tr style={{ background: 'var(--bg-secondary)' }}>
          {headers.map(h => <th key={h} style={{ ...thStyle, textAlign: 'left' }}>{h}</th>)}
        </tr>
      </thead>
      <tbody>{children}</tbody>
    </table>
  )
}

const thStyle: React.CSSProperties = { padding: '8px 16px', fontSize: 11, fontWeight: 700, textTransform: 'uppercase' as const, letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }
