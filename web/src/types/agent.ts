export interface Agent {
  id: string
  org_id: string
  name: string
  description?: string
  model_config_id?: string
  subagent_model_config_id?: string
  system_prompt?: string
  skill_ids: string[]
  skills?: Skill[]
  tool_ids: string[]
  all_builtin_tools?: boolean
  tools?: Tool[]
  mcp_server_ids: string[]
  mcp_servers: MCPServerOrg[]
  folder_id?: string
  max_turns?: number
  max_subagents?: number
  max_subagent_turns?: number
  model_config_params?: Record<string, unknown>
  created_by: string
  created_at: string
  updated_at: string
}

export interface MCPServerOrg {
  id: string
  org_id: string
  name: string
  type: 'stdio' | 'http'
  command: string
  args?: string[]
  created_by: string
  created_at: string
  updated_at: string
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
  price_per_input_token: number
  price_per_output_token: number
  price_per_cache_read_token: number
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
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
}

export type ToolType = 'builtin' | 'webhook' | 'sql_query'

export interface Tool {
  id: string
  org_id: string
  name: string
  description: string
  type: ToolType
  schema: Record<string, any>
  config: Record<string, any>
  require_confirmation?: boolean
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
  title: string | null
  ended_at?: string
  created_at: string
}

export interface AgentMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'tool' | 'compaction'
  content?: string
  tool_call_id?: string
  tool_calls?: ToolCall[]
  tokens_input?: number
  tokens_output?: number
  tokens_direct?: number
  image_ids?: string[]
  created_at: string
}

export interface ToolCall {
  id: string
  name: string
  arguments: Record<string, unknown>
  result?: unknown
  error?: string
  duration_ms?: number
  tokens_direct?: number
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

export interface TokenBreakdown {
  input: number
  output: number
  reasoning: number
  cache_read: number
  model_calls: number
  system_prompt: number
  skill_override: number
  history: number
  user_message: number
  tool_definitions: number
  tool_calls: number
  tool_results: number
  context_current?: number
  subagent_input?: number
  subagent_output?: number
  duration_ms?: number
}

export interface SessionUsage {
  input: number
  output: number
  reasoning: number
  cache_read: number
  model_calls: number
  subagent_input: number
  subagent_output: number
  context_tokens: number
  context_window: number
}

export type WSMessage =
  | { type: 'token'; data: string; seq?: number }
  | { type: 'reasoning'; data: string; seq?: number }
  | { type: 'tool_call'; tool: string; tool_call_id?: string; params: string; args: unknown; result: unknown; reasoning?: string; duration_ms?: number; seq?: number }
  | { type: 'tool_result'; tool: string; tool_call_id?: string; params: string; result: string; error?: string; duration_ms?: number; tokens_direct?: number; seq?: number }
  | { type: 'cell_created'; cell_id: string; position: number; seq?: number }
  | { type: 'cell_output'; cell_id: string; outputs: Array<{ type: string; data: unknown }>; seq?: number }
  | { type: 'cell_updated'; cell_id: string; seq?: number }
  | { type: 'subagent_progress'; tasks: SubagentTask[] }
  | { type: 'subagent_status'; task_id: string; status: string; goal?: string; result?: unknown; error?: string; duration_ms?: number; tokens_input?: number; tokens_output?: number; session_usage?: SessionUsage | null }
  | { type: 'subagent_message'; task_id: string; role: string; content: string; result?: string; tool_call_id?: string; tool_calls?: any; reasoning_content?: string; duration_ms?: number }
  | { type: 'done'; tokens?: TokenBreakdown; data?: { content?: string; reasoning?: string; tokens?: TokenBreakdown; session_usage?: SessionUsage | null }; seq?: number }
  | { type: 'error'; message: string }
  | { type: 'slash_result'; command: string; data: unknown }
  | { type: 'backpressure_warning'; dropped_tokens: number }
  | { type: 'reconnect_sync'; messages: AgentMessage[] | null; running?: boolean; server_seq?: number; session_usage?: SessionUsage | null }
  | { type: 'tasks_updated'; data: AgentTaskItem[] }
  | { type: 'tool_confirm_required'; tool_name: string; tool_args: string; current_source?: string }
  | { type: 'resync'; seq?: number }
  | { type: 'llm_retry'; attempt?: number; max_attempts?: number; error?: string; seq?: number }
  | { type: 'steering'; content: string; seq?: number }
  | { type: 'steering_accepted'; seq?: number }
  | { type: 'steering_busy'; content: string; seq?: number }
  | { type: 'question'; question: string; options?: Array<{ title: string; description?: string } | string>; allow_custom: boolean }
  | { type: 'token_update'; tokens: TokenBreakdown; session_usage?: SessionUsage | null }
  | { type: 'context_compacted'; summary: string; tokens?: TokenBreakdown }
  | { type: 'cancelled' }
