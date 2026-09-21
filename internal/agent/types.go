package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
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
	// ResolveTarget resolves the acting user's ClickHouse execution target for
	// a connector (the api.Server implementation is the same resolver HTTP
	// uses). It is wired by the API server because internal/agent cannot import
	// internal/api. A nil target with a nil error is equivalent to
	// executor.ErrUnmanagedConnector: the connector is not warehouse-managed
	// and the caller selects the legacy stored-credential path. Every other
	// outcome is a tool error and fails closed — managed ClickHouse execution
	// never falls back to the connector's stored credential.
	ResolveTarget func(ctx context.Context, userID, connectorID uuid.UUID, pinned bool) (*executor.ExecutionTarget, error)
	// ConnPool leases per-user ClickHouse connections for resolved targets. It
	// is required whenever ResolveTarget returns a managed target.
	ConnPool *executor.ConnPool
	// CheckPermissionFunc is the canonical ACL resolver (api.Server.checkPermission).
	// When set, CheckPermission delegates to it, so agent and MCP executions get
	// the same group-aware, admin-mode-aware authorization as HTTP. A nil
	// resolver makes CheckPermission fail closed; bare contexts (tests, tool
	// handlers invoked outside a server) must wire a stub explicitly.
	CheckPermissionFunc func(ctx context.Context, userID, orgID, orgRole, resourceType, resourceID, action string) (bool, error)
	// Running-state hooks wired to the notebook Hub (see internal/api/router.go
	// and mcp.go). They let agent-driven cell runs participate in the same
	// running/cancel lifecycle as user-triggered runs: badge, refresh-safe
	// sync replay, and the Cancel endpoint. All hooks are optional — check for
	// nil before use (bare contexts in tests and subagent runs leave them unset).
	SetRunningFunc   func(cellID, notebookID string, startedAt time.Time)
	UnsetRunningFunc func(cellID string)
	SetCancelFunc    func(cellID string, cancel context.CancelFunc)
	DeleteCancelFunc func(cellID string)
	QuestionFunc     func(question string, options any, allowCustom bool) (string, error)
	// OutputLimitsMaxBytes is the platform ceiling applied to the org's
	// cell_output_max_bytes cap for agent-driven executions (0 = no ceiling).
	OutputLimitsMaxBytes int64
}

type AgentTask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type EngineEvent struct {
	Type     string          `json:"type"`
	CellID   string          `json:"cell_id,omitempty"`
	Position int             `json:"position,omitempty"`
	Source   string          `json:"source,omitempty"`
	Tasks    []AgentTask     `json:"tasks,omitempty"`
	Outputs  any             `json:"outputs,omitempty"`
	ToolName string          `json:"tool_name,omitempty"`
	ToolArgs string          `json:"tool_args,omitempty"`
	Tokens   *TokenBreakdown `json:"tokens,omitempty"`
	// SessionUsage is the compaction-aware running snapshot of the session's
	// token accounting, attached to token_update events.
	SessionUsage *models.SessionUsage `json:"session_usage,omitempty"`
	Summary      string               `json:"summary,omitempty"`
	Question     string               `json:"question,omitempty"`
	Options      any                  `json:"options,omitempty"`
	AllowCustom  bool                 `json:"allow_custom,omitempty"`
	// Attempt/MaxAttempts/Error carry llm_retry progress (engine.go retry loop).
	Attempt     int    `json:"attempt,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
	Error       string `json:"error,omitempty"`
	// Content carries free-text event payloads (steering messages).
	Content string `json:"content,omitempty"`
}

type QuestionResult struct {
	Answer string
}

func (tc *ToolContext) EmitCellDeleted(cellID string) {
	evt := EngineEvent{Type: "cell_deleted", CellID: cellID}
	if tc.Events != nil {
		*tc.Events = append(*tc.Events, evt)
	}
	if tc.OnEvent != nil {
		tc.OnEvent(evt)
	}
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
	if tc.CheckPermissionFunc == nil {
		// Fail closed: without the shared resolver there is no way to evaluate
		// group memberships, folder ancestors, Everyone grants, or admin mode.
		// A bare context must never authorize a tool action.
		return fmt.Errorf("permission resolver not configured")
	}
	allowed, err := tc.CheckPermissionFunc(tc.Context, tc.UserID, tc.OrgID, tc.OrgRole, resourceType, resourceID, action)
	if err != nil {
		return fmt.Errorf("permission check: %w", err)
	}
	if !allowed {
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

type ResolvedCell struct {
	ID         string
	NotebookID string
}

func (tc *ToolContext) ResolveCell(cellID string) (*ResolvedCell, error) {
	if _, err := uuid.Parse(cellID); err == nil {
		var nbID string
		err := tc.DB.QueryRow(tc.Context, `SELECT notebook_id FROM cells WHERE id = $1`, cellID).Scan(&nbID)
		if err != nil {
			return nil, fmt.Errorf("get cell notebook: %w", err)
		}
		return &ResolvedCell{ID: cellID, NotebookID: nbID}, nil
	}

	pos, err := strconv.Atoi(cellID)
	if err != nil {
		return nil, fmt.Errorf("invalid cell_id: must be a UUID or position number")
	}

	if tc.NotebookID == "" {
		return nil, fmt.Errorf("cannot resolve cell position without a notebook context")
	}

	var actualID string
	err = tc.DB.QueryRow(tc.Context,
		`SELECT id FROM cells WHERE notebook_id = $1 AND position = $2`,
		tc.NotebookID, pos-1,
	).Scan(&actualID)
	if err != nil {
		return nil, fmt.Errorf("no cell at position %d in this notebook", pos)
	}

	return &ResolvedCell{ID: actualID, NotebookID: tc.NotebookID}, nil
}

func (tc *ToolContext) AuditLog(action, resourceType, resourceID string) error {
	return tc.AuditLogWithMetadata(action, resourceType, resourceID, nil)
}

// AuditLogWithMetadata records an audit entry and merges extra metadata into
// the agent-session envelope. It is used by execution paths that must record
// the warehouse identity a run actually used (mirroring the HTTP handler's
// cell.execute audit fields).
func (tc *ToolContext) AuditLogWithMetadata(action, resourceType, resourceID string, extra map[string]any) error {
	if tc.DB == nil {
		return nil
	}
	metadata := make(map[string]any, len(extra)+1)
	for k, v := range extra {
		metadata[k] = v
	}
	metadata["agent_session_id"] = tc.SessionID
	payload, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	_, err = tc.DB.Exec(tc.Context, `
		INSERT INTO audit_logs (org_id, user_id, action, resource_type, resource_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, tc.OrgID, tc.UserID, action, resourceType, resourceID, string(payload))
	return err
}

type ToolResult struct {
	CellID   string `json:"cell_id,omitempty"`
	Position int    `json:"position,omitempty"`
	Output   any    `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

type ToolHandler func(args json.RawMessage, ctx *ToolContext) (any, error)

// DefaultToolTimeout applies when a tool declares Timeout == 0 (e.g. a
// custom test tool). Tools should normally declare an explicit budget.
const DefaultToolTimeout = 120 * time.Second

// NoTimeout marks interactive/long-running tools that must not be wrapped.
const NoTimeout = -1 * time.Second

type ToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
	Handler         ToolHandler `json:"-"`
	ConfirmRequired bool        `json:"-"`
	// Timeout is the default execution budget. 0 falls back to
	// DefaultToolTimeout; NoTimeout (-1) disables wrapping.
	Timeout time.Duration `json:"-"`
	// TimeoutFromArgs overrides Timeout when the caller supplied one
	// (e.g. run_cell's timeout_ms).
	TimeoutFromArgs func(json.RawMessage) (time.Duration, bool) `json:"-"`
}

// Execute runs the handler under the effective timeout and normalizes
// wrapper-caused deadline errors.
func (t *ToolDef) Execute(args json.RawMessage, tc *ToolContext) (any, error) {
	if t.Timeout < 0 {
		return t.Handler(args, tc)
	}
	timeout := t.Timeout
	if t.TimeoutFromArgs != nil {
		if d, ok := t.TimeoutFromArgs(args); ok && d > 0 {
			timeout = d
		}
	}
	if timeout == 0 {
		timeout = DefaultToolTimeout
	}
	runCtx, cancel := context.WithTimeout(tc.Context, timeout)
	defer cancel()
	tcCopy := *tc
	tcCopy.Context = runCtx
	result, err := t.Handler(args, &tcCopy)
	if err != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) && tc.Context.Err() == nil {
		return result, fmt.Errorf("tool %q timed out after %s", t.Function.Name, timeout)
	}
	return result, err
}

func normalizeToolParams(params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	if _, ok := params["type"]; !ok {
		params["type"] = "object"
	}
	if _, ok := params["properties"]; !ok {
		params["properties"] = map[string]any{}
	}
	return params
}

func (t *ToolDef) ToOpenAITool() (OpenAITool, error) {
	var params map[string]any
	if t.Function.Parameters != nil {
		switch v := t.Function.Parameters.(type) {
		case string:
			if err := json.Unmarshal([]byte(v), &params); err != nil {
				return OpenAITool{}, fmt.Errorf("parse parameters: %w", err)
			}
		case map[string]any:
			params = v
		case models.JSONMap:
			params = map[string]any(v)
		}
	}
	params = normalizeToolParams(params)
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

func validateRequiredParams(schema any, params map[string]any) error {
	var schemaMap map[string]any
	switch v := schema.(type) {
	case map[string]any:
		schemaMap = v
	case models.JSONMap:
		schemaMap = map[string]any(v)
	default:
		return nil
	}
	required, _ := schemaMap["required"].([]any)
	if len(required) == 0 {
		return nil
	}
	var missing []string
	for _, r := range required {
		name, _ := r.(string)
		if name == "" {
			continue
		}
		if _, ok := params[name]; !ok || params[name] == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required parameters: %s", strings.Join(missing, ", "))
	}
	return nil
}
