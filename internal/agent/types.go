package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ToolContext struct {
	Context          context.Context
	UserID           string
	OrgID            string
	OrgRole          string
	NotebookID       string
	SessionID        string
	DB               *pgxpool.Pool
	TurnCount        int
	CumulativeTokens int
	Events           *[]EngineEvent
	MasterKey        []byte
	OnEvent          func(EngineEvent)
	BroadcastFunc    func(notebookID string, msg any)
}

type AgentTask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type EngineEvent struct {
	Type     string      `json:"type"`
	CellID   string      `json:"cell_id,omitempty"`
	Position int         `json:"position,omitempty"`
	Source   string      `json:"source,omitempty"`
	Tasks    []AgentTask `json:"tasks,omitempty"`
	Outputs  any         `json:"outputs,omitempty"`
}

func (tc *ToolContext) EmitCellCreated(cellID string, position int) {
	evt := EngineEvent{Type: "cell_created", CellID: cellID, Position: position}
	if tc.Events != nil {
		*tc.Events = append(*tc.Events, evt)
	}
	if tc.OnEvent != nil {
		tc.OnEvent(evt)
	}
}

func (tc *ToolContext) EmitTasksUpdated(tasks []AgentTask) {
	evt := EngineEvent{Type: "tasks_updated", Tasks: tasks}
	if tc.Events != nil {
		*tc.Events = append(*tc.Events, evt)
	}
	if tc.OnEvent != nil {
		tc.OnEvent(evt)
	}
}

func (tc *ToolContext) EmitCellOutput(cellID string, outputs any) {
	evt := EngineEvent{Type: "cell_output", CellID: cellID, Outputs: outputs}
	if tc.Events != nil {
		*tc.Events = append(*tc.Events, evt)
	}
	if tc.OnEvent != nil {
		tc.OnEvent(evt)
	}
}

func (tc *ToolContext) EmitCellUpdated(cellID string, source string) {
	evt := EngineEvent{Type: "cell_updated", CellID: cellID, Source: source}
	if tc.Events != nil {
		*tc.Events = append(*tc.Events, evt)
	}
	if tc.OnEvent != nil {
		tc.OnEvent(evt)
	}
}

func (tc *ToolContext) CheckPermission(resourceType, resourceID, action string) error {
	if tc.OrgRole == "admin" {
		return nil
	}

	var exists bool
	err := tc.DB.QueryRow(tc.Context, `
		SELECT EXISTS(
			SELECT 1 FROM acl_entries
			WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3
			AND (
				(subject_type = 'user' AND subject_id = $4)
				OR (subject_type = 'org_role' AND subject_id = $5)
				OR (subject_type = 'org_role' AND subject_id = 'everyone')
			)
			AND $6 = ANY(actions)
		)
	`, resourceType, resourceID, tc.OrgID, tc.UserID, tc.OrgRole, action).Scan(&exists)
	if err != nil {
		return fmt.Errorf("permission check: %w", err)
	}
	if !exists {
		return fmt.Errorf("permission denied: %s on %s/%s", action, resourceType, resourceID)
	}
	return nil
}

func (tc *ToolContext) GetNotebookIDForCell(cellID string) (string, error) {
	var notebookID string
	err := tc.DB.QueryRow(tc.Context, `SELECT notebook_id FROM cells WHERE id = $1`, cellID).Scan(&notebookID)
	if err != nil {
		return "", fmt.Errorf("get cell notebook: %w", err)
	}
	return notebookID, nil
}

func (tc *ToolContext) AuditLog(action, resourceType, resourceID string) error {
	if tc.DB == nil {
		return nil
	}
	_, err := tc.DB.Exec(tc.Context, `
		INSERT INTO audit_logs (org_id, user_id, action, resource_type, resource_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, tc.OrgID, tc.UserID, action, resourceType, resourceID, fmt.Sprintf(`{"agent_session_id": "%s"}`, tc.SessionID))
	return err
}

type ToolResult struct {
	CellID   string `json:"cell_id,omitempty"`
	Position int    `json:"position,omitempty"`
	Output   any    `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

type ToolHandler func(args json.RawMessage, ctx *ToolContext) (any, error)

type ToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
	Timeout time.Duration
	Handler ToolHandler `json:"-"`
}

func (t *ToolDef) ToOpenAITool() (OpenAITool, error) {
	var params map[string]any
	if t.Function.Parameters != nil {
		if s, ok := t.Function.Parameters.(string); ok {
			if err := json.Unmarshal([]byte(s), &params); err != nil {
				return OpenAITool{}, fmt.Errorf("parse parameters: %w", err)
			}
		} else if m, ok := t.Function.Parameters.(map[string]any); ok {
			params = m
		}
	}
	return OpenAITool{
		Type: "function",
		Function: struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		}{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  params,
		},
	}, nil
}

type ToolRegistry struct {
	tools map[string]*ToolDef
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]*ToolDef)}
}

func (r *ToolRegistry) Register(def *ToolDef) {
	if def.Timeout == 0 {
		def.Timeout = 30 * time.Second
	}
	if def.Type == "" {
		def.Type = "function"
	}
	r.tools[def.Function.Name] = def
}

func (r *ToolRegistry) Get(name string) (*ToolDef, bool) {
	def, ok := r.tools[name]
	return def, ok
}

func (r *ToolRegistry) List() []*ToolDef {
	defs := make([]*ToolDef, 0, len(r.tools))
	for _, def := range r.tools {
		defs = append(defs, def)
	}
	return defs
}
