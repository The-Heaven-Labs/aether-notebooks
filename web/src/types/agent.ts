export interface Agent {
  id: string
  org_id: string
  name: string
  description?: string
  model_config_id?: string
  subagent_model_config_id?: string
  system_prompt?: string
  skill_ids: string[]
  mcp_servers: MCPServer[]
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface MCPServer {
  name: string
  type: 'stdio' | 'http'
  command: string
  args?: string[]
}

export interface ModelConfig {
  id: string
  org_id: string
  name: string
  provider: string
  base_url: string
  model: string
  default_params?: Record<string, unknown>
  context_window: number
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface Skill {
  id: string
  org_id: string
  name: string
  description?: string
  system_prompt?: string
  tool_ids: string[]
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface AgentSession {
  id: string
  agent_id: string
  notebook_id: string
  user_id: string
  max_turns: number
  max_tokens: number
  ended_at?: string
  created_at: string
}

export interface AgentMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'tool'
  content?: string
  tool_call_id?: string
  tool_calls?: ToolCall[]
  tokens_input?: number
  tokens_output?: number
  created_at: string
}

export interface ToolCall {
  id: string
  name: string
  arguments: Record<string, unknown>
  result?: unknown
  error?: string
  duration_ms?: number
}

export interface SubagentTask {
  id: string
  goal: string
  status: 'queued' | 'running' | 'completed' | 'failed'
  result?: unknown
}

export interface AgentTaskItem {
  id: string
  description: string
  status: 'pending' | 'in_progress' | 'done'
}

export type WSMessage =
  | { type: 'token'; data: string }
  | { type: 'reasoning'; data: string }
  | { type: 'tool_call'; tool: string; args: unknown; result: unknown; reasoning?: string }
  | { type: 'tool_result'; tool: string; params: string; result: string; error?: string }
  | { type: 'cell_created'; cell_id: string; position: number }
  | { type: 'subagent_progress'; tasks: SubagentTask[] }
  | { type: 'done'; tokens_used?: number; data?: { content?: string; reasoning?: string } }
  | { type: 'error'; message: string }
  | { type: 'slash_result'; command: string; data: unknown }
  | { type: 'backpressure_warning'; dropped_tokens: number }
  | { type: 'reconnect_sync'; messages: AgentMessage[] }
  | { type: 'tasks_updated'; data: AgentTaskItem[] }
