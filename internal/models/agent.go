package models

import (
	"time"
)

type ModelConfig struct {
	ID                     string    `json:"id"`
	OrgID                  string    `json:"org_id"`
	Name                   string    `json:"name"`
	Provider               string    `json:"provider"`
	BaseURL                string    `json:"base_url"`
	Model                  string    `json:"model"`
	APIKeyEncrypted        []byte    `json:"-"`
	APIKeyEnvVar           *string   `json:"-"`
	DefaultParams          JSONMap   `json:"default_params"`
	ContextWindow          int       `json:"context_window"`
	PricePerInputToken     float64   `json:"price_per_input_token"`
	PricePerOutputToken    float64   `json:"price_per_output_token"`
	PricePerCacheReadToken float64   `json:"price_per_cache_read_token"`
	FolderID               *string   `json:"folder_id,omitempty"`
	CreatedBy              string    `json:"created_by"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type Skill struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	SystemPrompt string    `json:"system_prompt,omitempty"`
	FolderID     *string   `json:"folder_id,omitempty"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type MCPServerOrg struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Command   string    `json:"command"`
	Args      []string  `json:"args,omitempty"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ToolType string

const (
	ToolTypeBuiltin  ToolType = "builtin"
	ToolTypeWebhook  ToolType = "webhook"
	ToolTypeSQLQuery ToolType = "sql_query"
)

type Tool struct {
	ID                  string    `json:"id"`
	OrgID               string    `json:"org_id"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	Type                ToolType  `json:"type"`
	Schema              JSONMap   `json:"schema"`
	Config              JSONMap   `json:"config"`
	RequireConfirmation bool      `json:"require_confirmation"`
	FolderID            *string   `json:"folder_id,omitempty"`
	CreatedBy           string    `json:"created_by"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Agent struct {
	ID                    string         `json:"id"`
	OrgID                 string         `json:"org_id"`
	Name                  string         `json:"name"`
	Description           string         `json:"description,omitempty"`
	ModelConfigID         *string        `json:"model_config_id,omitempty"`
	SubagentModelConfigID *string        `json:"subagent_model_config_id,omitempty"`
	SystemPrompt          string         `json:"system_prompt,omitempty"`
	SkillIDs              []string       `json:"skill_ids"`
	Skills                []Skill        `json:"skills,omitempty"`
	MCPServerIDs          []string       `json:"mcp_server_ids"`
	MCPServers            []MCPServerOrg `json:"mcp_servers"`
	ToolIDs               []string       `json:"tool_ids,omitempty"`
	AllBuiltinTools       bool           `json:"all_builtin_tools"`
	Tools                 []Tool         `json:"tools,omitempty"`
	ModelConfigParams     JSONMap        `json:"model_config_params,omitempty"`
	FolderID              *string        `json:"folder_id,omitempty"`
	MaxTurns              *int           `json:"max_turns,omitempty"`
	MaxSubAgents          int            `json:"max_subagents,omitempty"`
	MaxSubagentTurns      int            `json:"max_subagent_turns,omitempty"`
	CreatedBy             string         `json:"created_by"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type AgentSession struct {
	ID         string     `json:"id"`
	AgentID    string     `json:"agent_id"`
	NotebookID string     `json:"notebook_id"`
	UserID     string     `json:"user_id"`
	MaxTurns   int        `json:"max_turns"`
	Title      *string    `json:"title,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type ToolCall struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Arguments  any     `json:"arguments"`
	Result     any     `json:"result,omitempty"`
	Error      *string `json:"error,omitempty"`
	DurationMs int     `json:"duration_ms,omitempty"`
}

type AgentMessage struct {
	ID               string     `json:"id"`
	SessionID        string     `json:"session_id"`
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ToolCallID       *string    `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	TokensInput      int        `json:"tokens_input,omitempty"`
	TokensOutput     int        `json:"tokens_output,omitempty"`
	TokensReasoning  int        `json:"tokens_reasoning,omitempty"`
	ModelCalls       int        `json:"model_calls,omitempty"`
	DurationMs       int        `json:"duration_ms,omitempty"`
	ImageIDs         []string   `json:"image_ids,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type SubagentTask struct {
	ID              string     `json:"id"`
	ParentSessionID string     `json:"parent_session_id"`
	ParentMessageID *string    `json:"parent_message_id,omitempty"`
	AgentID         *string    `json:"agent_id,omitempty"`
	Goal            string     `json:"goal"`
	Context         JSONMap    `json:"context,omitempty"`
	Status          string     `json:"status"`
	Result          JSONMap    `json:"result,omitempty"`
	TokensInput     int        `json:"tokens_input,omitempty"`
	TokensOutput    int        `json:"tokens_output,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type AgentStatsDaily struct {
	Date          string `json:"date"`
	AgentID       string `json:"agent_id"`
	UserID        string `json:"user_id"`
	SessionsCount int    `json:"sessions_count"`
	MessagesCount int    `json:"messages_count"`
	TokensInput   int64  `json:"tokens_input"`
	TokensOutput  int64  `json:"tokens_output"`
}

type AgentVersion struct {
	ID            string    `json:"id"`
	AgentID       string    `json:"agent_id"`
	Version       int       `json:"version"`
	Name          *string   `json:"name,omitempty"`
	Description   *string   `json:"description,omitempty"`
	SystemPrompt  *string   `json:"system_prompt,omitempty"`
	SkillIDs      []string  `json:"skill_ids,omitempty"`
	ModelConfigID *string   `json:"model_config_id,omitempty"`
	ChangedBy     string    `json:"changed_by"`
	ChangeReason  string    `json:"change_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type JSONMap map[string]any
