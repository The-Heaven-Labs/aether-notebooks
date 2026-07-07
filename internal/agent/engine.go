package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/models"
	"github.com/the-heaven-labs/aether/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Engine struct {
	registry           *ToolRegistry
	session            *SessionStore
	llm                *LLMClient
	pool               *pgxpool.Pool
	mu                 sync.Mutex
	BroadcastFunc      func(notebookID string, msg any)
	toolAllowedDomains []string
	tokenCounter       *TokenCounter
	store              storage.Storage
	reasoningEffort        sync.Map // sessionID -> string
	toolConfirmPending     sync.Map // sessionID -> chan ToolConfirmResult
	pageContextMap         sync.Map // sessionID -> map[string]string
	sessionModelConfig     sync.Map // sessionID -> modelConfigID string
	frontendURL            string
	streams            *StreamManager
}

type ToolConfirmResult struct {
	Approved bool
	ToolName string
}

func (e *Engine) SetReasoningEffort(sessionID, effort string) {
	e.reasoningEffort.Store(sessionID, effort)
}

func (e *Engine) GetReasoningEffort(sessionID string) string {
	if v, ok := e.reasoningEffort.Load(sessionID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (e *Engine) SetSessionModelConfig(sessionID, modelConfigID string) {
	e.sessionModelConfig.Store(sessionID, modelConfigID)
}

func (e *Engine) GetSessionModelConfig(sessionID string) string {
	if v, ok := e.sessionModelConfig.Load(sessionID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

type PageContextInfo struct {
	Type  string
	ID    string
	Title string
}

func (e *Engine) SetPageContext(sessionID string, pc *PageContextInfo) {
	e.pageContextMap.Store(sessionID, pc)
}

func (e *Engine) GetPageContext(sessionID string) *PageContextInfo {
	if v, ok := e.pageContextMap.Load(sessionID); ok {
		if pc, ok := v.(*PageContextInfo); ok {
			return pc
		}
	}
	return nil
}

func (e *Engine) SetToolConfirm(sessionID string, ch chan ToolConfirmResult) {
	e.toolConfirmPending.Store(sessionID, ch)
}

func (e *Engine) ResolveToolConfirm(sessionID string, approved bool, toolName string) {
	v, ok := e.toolConfirmPending.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	if ch, ok := v.(chan ToolConfirmResult); ok {
		ch <- ToolConfirmResult{Approved: approved, ToolName: toolName}
	}
}

func NewEngine(ctx context.Context, pool *pgxpool.Pool) *Engine {
	engine := &Engine{
		registry:     NewToolRegistry(),
		session:      NewSessionStore(pool),
		pool:         pool,
		tokenCounter: NewTokenCounter(),
		streams:      NewStreamManager(),
	}

	RegisterNotebookTools(engine.registry, pool)
	RegisterAgentTools(engine.registry, pool, engine)
	RegisterPlatformTools(engine.registry, pool)
	RegisterChartTools(engine.registry, pool)
	RegisterManageTools(engine.registry, pool)

	// Seed built-in tools for all orgs
	orgRows, err := pool.Query(ctx, `SELECT id FROM orgs`)
	if err == nil {
		for orgRows.Next() {
			var orgID string
			orgRows.Scan(&orgID)
			SeedBuiltinTools(ctx, pool, orgID)
		}
		orgRows.Close()
	}

	return engine
}

// compactChatHistory replaces older conversation history with an LLM-generated summary
// when the token count approaches the context window limit.
// It keeps the system prompt (index 0) and the last 8 messages, summarizing everything in between.
func (e *Engine) compactChatHistory(ctx context.Context, llm *LLMClient, chatMsgs []ChatMessage, masterKey []byte, sessionID string) []ChatMessage {
	if len(chatMsgs) <= 10 {
		return chatMsgs
	}

	// Keep system message (index 0) and last 8 messages (recent context + current turn)
	keepEnd := len(chatMsgs) - 8
	if keepEnd <= 1 {
		return chatMsgs
	}

	oldMsgs := chatMsgs[1:keepEnd]

	var sb strings.Builder
	sb.WriteString("Summarize the following conversation history concisely, preserving key information, decisions, data findings, and context:\n\n")
	for _, m := range oldMsgs {
		role := m.Role
		content := m.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", role, content))
	}

	resp, err := llm.Chat(ctx, []ChatMessage{
		{Role: "user", Content: sb.String()},
	}, nil, masterKey)
	if err != nil {
		slog.Warn("compaction summarization failed", "session_id", sessionID, "error", err)
		return chatMsgs
	}

	summary := ""
	if len(resp.Choices) > 0 {
		summary = resp.Choices[0].Message.Content
	}
	if summary == "" {
		return chatMsgs
	}

	compacted := make([]ChatMessage, 0, keepEnd+8)
	compacted = append(compacted, chatMsgs[0])
	compacted = append(compacted, ChatMessage{
		Role:    "system",
		Content: "The following is a summary of earlier conversation history:\n\n" + summary + "\n\n(older context was compacted to stay within context window limits)",
	})
	compacted = append(compacted, chatMsgs[keepEnd:]...)

	slog.Info("context compaction completed", "session_id", sessionID, "old_msgs", len(oldMsgs), "new_msgs", len(compacted))
	return compacted
}

// sanitizeChatMessages ensures every assistant message with tool_calls has
// matching tool messages, and removes orphaned tool messages. This prevents
// LLM API 400 errors about broken tool message pairing.
func sanitizeChatMessages(msgs []ChatMessage) []ChatMessage {
	for ci := 0; ci < len(msgs); ci++ {
		if msgs[ci].Role != "assistant" || len(msgs[ci].ToolCalls) == 0 {
			continue
		}
		expected := make(map[string]bool)
		for _, tc := range msgs[ci].ToolCalls {
			expected[tc.ID] = true
		}
		for j := ci + 1; j < len(msgs); j++ {
			if msgs[j].Role == "tool" && msgs[j].ToolCallID != "" {
				delete(expected, msgs[j].ToolCallID)
			} else if msgs[j].Role == "assistant" || msgs[j].Role == "user" {
				break
			}
		}
		for id := range expected {
			placeholder := ChatMessage{
				Role:       "tool",
				ToolCallID: id,
				Content:    "Tool call was interrupted and did not complete.",
			}
			insertAt := ci + 1
			for insertAt < len(msgs) && msgs[insertAt].Role == "tool" {
				insertAt++
			}
			msgs = append(msgs, ChatMessage{})
			copy(msgs[insertAt+1:], msgs[insertAt:])
			msgs[insertAt] = placeholder
		}
	}
	// Remove orphaned tool messages (tool messages without a preceding
	// assistant message with matching tool_calls).
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "tool" || msgs[i].ToolCallID == "" {
			continue
		}
		hasMatch := false
		for j := i - 1; j >= 0; j-- {
			if msgs[j].Role == "assistant" && len(msgs[j].ToolCalls) > 0 {
				for _, tc := range msgs[j].ToolCalls {
					if tc.ID == msgs[i].ToolCallID {
						hasMatch = true
						break
					}
				}
			}
			if hasMatch {
				break
			}
			if msgs[j].Role == "user" {
				break
			}
		}
		if !hasMatch {
			msgs = append(msgs[:i], msgs[i+1:]...)
		}
	}
	return msgs
}

func (e *Engine) ProcessMessage(ctx context.Context, sessionID string, userMessage string, imageIDs []string, tools []*ToolDef, masterKey []byte, capturedPageContext *PageContextInfo, onToken func(string), onReasoning func(string), onToolCall func(string, string, string, string, int), onToolResult func(string, string, string, string, int), onEvent func(EngineEvent)) (string, string, []models.ToolCall, []EngineEvent, *TokenBreakdown, error) {
	var events []EngineEvent
	slog.Debug("engine: ProcessMessage start", "session_id", sessionID, "msg_len", len(userMessage), "image_count", len(imageIDs))

	// Resolve image data URIs for the current user message
	imageDataURIs, _ := e.FetchImageDataURIs(ctx, imageIDs)
	if len(imageDataURIs) > 0 {
		slog.Debug("engine: resolved images for user message", "session_id", sessionID, "count", len(imageDataURIs))
	}
	session, err := e.session.GetSession(ctx, sessionID)
	if err != nil {
		slog.Error("engine: get session failed", "session_id", sessionID, "error", err)
		return "", "", nil, events, nil, fmt.Errorf("get session: %w", err)
	}

	var agent models.Agent
	var systemPrompt string
	var skillIDs []byte
	var toolIDs []byte
	err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, description, model_config_id, subagent_model_config_id, system_prompt, array_to_json(skill_ids)::text, array_to_json(tool_ids)::text, folder_id, max_turns, created_by, created_at, updated_at FROM agents WHERE id = $1`, session.AgentID).Scan(
		&agent.ID, &agent.OrgID, &agent.Name, &agent.Description, &agent.ModelConfigID, &agent.SubagentModelConfigID, &systemPrompt, &skillIDs, &toolIDs, &agent.FolderID, &agent.MaxTurns, &agent.CreatedBy, &agent.CreatedAt, &agent.UpdatedAt)
	if err != nil {
		return "", "", nil, events, nil, fmt.Errorf("get agent: %w", err)
	}
	if skillIDs != nil {
		json.Unmarshal(skillIDs, &agent.SkillIDs)
	}
	if toolIDs != nil {
		json.Unmarshal(toolIDs, &agent.ToolIDs)
	}

	mcpRows, err := e.pool.Query(ctx, `
		SELECT ms.id, ms.org_id, ms.name, ms.type, ms.command, ms.args, ms.created_by, ms.created_at, ms.updated_at
		FROM agent_mcp_servers ams
		JOIN mcp_servers ms ON ms.id = ams.mcp_server_id
		WHERE ams.agent_id = $1
		ORDER BY ms.name
	`, session.AgentID)
	if err == nil {
		for mcpRows.Next() {
			var s models.MCPServerOrg
			var args []byte
			if err := mcpRows.Scan(&s.ID, &s.OrgID, &s.Name, &s.Type, &s.Command, &args, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err == nil {
				if args != nil {
					json.Unmarshal(args, &s.Args)
				}
				agent.MCPServerIDs = append(agent.MCPServerIDs, s.ID)
				agent.MCPServers = append(agent.MCPServers, s)
			}
		}
		if err := mcpRows.Err(); err != nil {
			slog.Warn("error iterating mcp server rows", "session_id", sessionID, "error", err)
		}
		mcpRows.Close()
	}

	var orgRole string
	err = e.pool.QueryRow(ctx, `SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, agent.OrgID, session.UserID).Scan(&orgRole)
	if err != nil {
		orgRole = "editor"
	}

	// Load agent tools from tools table
	agentTools := make([]*ToolDef, 0)
	if len(agent.ToolIDs) > 0 {
		tRows, err := e.pool.Query(ctx, `
			SELECT id, org_id, name, description, type, schema, config
			FROM tools WHERE id = ANY($1)`, agent.ToolIDs)
		if err != nil {
			slog.Warn("engine: failed to query agent tools", "session_id", sessionID, "error", err)
		} else {
			for tRows.Next() {
				var t models.Tool
				var schema, config []byte
				if err := tRows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &t.Type, &schema, &config); err != nil {
					continue
				}
				if schema != nil {
					json.Unmarshal(schema, &t.Schema)
				}
				if t.Schema == nil {
					t.Schema = models.JSONMap{}
				}
				if config != nil {
					json.Unmarshal(config, &t.Config)
				}
				// Check runtime permission: user must have 'use' on the tool
				if orgRole != "admin" {
					allowed, err := e.checkToolUsePermission(ctx, session.UserID, agent.OrgID, orgRole, t.ID)
					if err != nil || !allowed {
						slog.Warn("engine: user lacks use permission for tool", "tool", t.Name, "user", session.UserID, "error", err)
						continue
					}
				}
				toolDef, err := e.resolveToolDef(&t)
				if err != nil {
					slog.Warn("engine: failed to resolve tool", "tool", t.Name, "error", err)
					continue
				}
				if toolDef != nil {
					agentTools = append(agentTools, toolDef)
				}
			}
			tRows.Close()
		}
	}

	// Build skill catalog (name + description only, not full content)
	type skillCatalogEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var skillCatalog []skillCatalogEntry
	if len(agent.SkillIDs) > 0 {
		rows, err := e.pool.Query(ctx, `SELECT name, description FROM skills WHERE id = ANY($1) AND description IS NOT NULL AND description != ''`, agent.SkillIDs)
		if err == nil {
			for rows.Next() {
				var entry skillCatalogEntry
				if err := rows.Scan(&entry.Name, &entry.Description); err == nil {
					skillCatalog = append(skillCatalog, entry)
				}
			}
			rows.Close()
		}
	}

	llmClient := e.llm
	contextWindow := 128000 // default
	compactionThreshold := 70 // default 70%
	modelName := ""
	modelConfigID := e.GetSessionModelConfig(sessionID)
	if modelConfigID == "" {
		if agent.ModelConfigID != nil && *agent.ModelConfigID != "" {
			modelConfigID = *agent.ModelConfigID
		}
	}
	if modelConfigID != "" {
		slog.Debug("engine: using agent model config", "session_id", sessionID, "model_config_id", modelConfigID)
		var mc models.ModelConfig
		var defaultParams []byte
		err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, folder_id, created_by, created_at, updated_at FROM model_configs WHERE id = $1`, modelConfigID).Scan(
			&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &mc.APIKeyEncrypted, &defaultParams, &mc.ContextWindow, &mc.FolderID, &mc.CreatedBy, &mc.CreatedAt, &mc.UpdatedAt)
		if err != nil {
			return "", "", nil, events, nil, fmt.Errorf("get model config: %w", err)
		}
		if defaultParams != nil {
			json.Unmarshal(defaultParams, &mc.DefaultParams)
		}
		if v, ok := mc.DefaultParams["compaction_threshold"]; ok {
			if f, ok := v.(float64); ok {
				compactionThreshold = int(f)
			}
		}
		modelName = mc.Model
		if effort := e.GetReasoningEffort(sessionID); effort != "" {
			if mc.DefaultParams == nil {
				mc.DefaultParams = make(map[string]any)
			}
			mc.DefaultParams["reasoning_effort"] = effort
		}
		llmClient = NewLLMClient(mc.BaseURL, mc.Model, mc.APIKeyEncrypted, mc.DefaultParams)
		contextWindow = mc.ContextWindow
	} else {
		slog.Warn("engine: no model config on agent, using default LLM client", "session_id", sessionID, "default_llm_nil", e.llm == nil)
	}

	if llmClient == nil {
		return "", "", nil, events, nil, fmt.Errorf("no LLM client available: assign a model config to this agent in the Agents page")
	}

	messages, err := e.session.GetMessages(ctx, sessionID)
	if err != nil {
		return "", "", nil, events, nil, fmt.Errorf("get messages: %w", err)
	}

	// Handle /skill:<name> prefix — inject skill prompt for this turn only
	var skillOverridePrompt string
	effectiveMessage := userMessage
	if strings.HasPrefix(userMessage, "/skill:") {
		parts := strings.SplitN(userMessage, " ", 2)
		skillName := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(parts[0], "/skill:")))
		skillName = strings.ReplaceAll(skillName, " ", "-")

		var skillPrompt string
		err = e.pool.QueryRow(ctx, `SELECT system_prompt FROM skills WHERE org_id = $1 AND LOWER(REPLACE(name, ' ', '-')) = $2`, agent.OrgID, skillName).Scan(&skillPrompt)
		if err != nil {
			return "", "", nil, events, nil, fmt.Errorf("skill '%s' not found", skillName)
		}
		skillOverridePrompt = skillPrompt
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			effectiveMessage = strings.TrimSpace(parts[1])
		} else {
			effectiveMessage = "Use the skill instructions above."
		}
	}

	// Build skill catalog for system prompt (YAML format)
	skillCatalogStr := ""
	if len(skillCatalog) > 0 {
		var sb strings.Builder
		sb.WriteString("\n\nThe following skills provide specialized instructions for specific tasks.")
		sb.WriteString("\nWhen a task matches a skill description, use the `load_skill` tool to load the full instructions before proceeding.")
		sb.WriteString("\n\navailable_skills:")
		for _, skill := range skillCatalog {
			sb.WriteString(fmt.Sprintf("\n  - name: %s", skill.Name))
			sb.WriteString(fmt.Sprintf("\n    description: %s", skill.Description))
		}
		skillCatalogStr = sb.String()
	}

	chatMsgs := make([]ChatMessage, 0)
	notebookCtx := e.buildNotebookContext(ctx, session.NotebookID)
	pageContextStr := ""
	pc := capturedPageContext
	if pc == nil {
		pc = e.GetPageContext(sessionID)
	}
	if pc != nil {
		pageContextStr = fmt.Sprintf("\n\nCurrent page: %s", pc.Type)
		if pc.ID != "" {
			pageContextStr += fmt.Sprintf(" (id: %s)", pc.ID)
		}
		if pc.Title != "" {
			pageContextStr += fmt.Sprintf(" (title: %q)", pc.Title)
		}
		pageContextStr += "\nUse this context to understand what the user is looking at."
	}

	if systemPrompt != "" {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: systemPrompt + notebookCtx + skillCatalogStr + pageContextStr})
	} else {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: notebookCtx + skillCatalogStr + pageContextStr})
	}
	// Pre-fetch image data URIs for historical messages that have image IDs
	type histImages struct {
		ids  []string
		uris []string
	}
	histImageMap := make(map[int]*histImages)
	for i, m := range messages {
		if m.Role == "user" && len(m.ImageIDs) > 0 {
			uris, _ := e.FetchImageDataURIs(ctx, m.ImageIDs)
			histImageMap[i] = &histImages{ids: m.ImageIDs, uris: uris}
		}
	}

	for i, m := range messages {
		if m.Role == "assistant" {
			if len(m.ToolCalls) > 0 && m.Content == "" {
				// Tool-calling assistant message without content — keep as-is
				msg := ChatMessage{Role: "assistant", ToolCalls: make([]ToolCall, 0, len(m.ToolCalls))}
				if m.ReasoningContent != "" {
					msg.ReasoningContent = m.ReasoningContent
				}
				for _, tc := range m.ToolCalls {
					argsStr := ""
					if b, err := json.Marshal(tc.Arguments); err == nil {
						argsStr = string(b)
					}
					msg.ToolCalls = append(msg.ToolCalls, ToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      tc.Name,
							Arguments: argsStr,
						},
					})
				}
				chatMsgs = append(chatMsgs, msg)
			} else if m.Content != "" {
				chatMsgs = append(chatMsgs, ChatMessage{Role: "assistant", Content: m.Content})
			}
		} else if m.Role == "tool" {
			toolID := ""
			if m.ToolCallID != nil {
				toolID = *m.ToolCallID
			}
			chatMsgs = append(chatMsgs, ChatMessage{Role: "tool", ToolCallID: toolID, Content: m.Content})
		} else if m.Role == "user" {
			if hi, ok := histImageMap[i]; ok && len(hi.uris) > 0 {
				parts := make([]ContentPart, 0, 1+len(hi.uris))
				if m.Content != "" {
					parts = append(parts, ContentPart{Type: "text", Text: m.Content})
				}
				for _, uri := range hi.uris {
					parts = append(parts, ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: uri}})
				}
				chatMsgs = append(chatMsgs, ChatMessage{Role: "user", MultiContent: parts})
			} else {
				chatMsgs = append(chatMsgs, ChatMessage{Role: "user", Content: m.Content})
			}
		} else {
			chatMsgs = append(chatMsgs, ChatMessage{Role: m.Role, Content: m.Content})
		}
	}
	chatMsgs = sanitizeChatMessages(chatMsgs)

	if skillOverridePrompt != "" {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: "# Active Skill\n\n" + skillOverridePrompt})
	}
	if len(imageDataURIs) > 0 {
		parts := make([]ContentPart, 0, 1+len(imageDataURIs))
		if effectiveMessage != "" {
			parts = append(parts, ContentPart{Type: "text", Text: effectiveMessage})
		}
		for _, uri := range imageDataURIs {
			parts = append(parts, ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: uri}})
		}
		chatMsgs = append(chatMsgs, ChatMessage{Role: "user", MultiContent: parts})
	} else {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "user", Content: effectiveMessage})
	}

	userMsgID := uuid.New().String()
	e.session.AppendMessage(ctx, &models.AgentMessage{
		ID:        userMsgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   effectiveMessage,
		ImageIDs:  imageIDs,
		CreatedAt: time.Now(),
	})

	// Count user messages and trigger title generation on 2nd
	userMsgCount := 0
	for _, m := range messages {
		if m.Role == "user" {
			userMsgCount++
		}
	}
	if userMsgCount == 1 { // This is the 2nd user message (messages didn't include the one we just appended)
		mk := make([]byte, len(masterKey))
		copy(mk, masterKey)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("engine: panic in title generation", "session_id", sessionID, "recover", r)
				}
			}()
			titleCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := e.generateSessionTitle(titleCtx, sessionID, mk); err != nil {
				slog.Warn("engine: failed to generate session title", "session_id", sessionID, "error", err)
			}
		}()
	}

	allTools := make([]*ToolDef, 0)
	seen := make(map[string]bool, len(agentTools)+len(tools))
	for _, t := range agentTools {
		if !seen[t.Function.Name] {
			seen[t.Function.Name] = true
			allTools = append(allTools, t)
		}
	}
	for _, t := range tools {
		if !seen[t.Function.Name] {
			seen[t.Function.Name] = true
			allTools = append(allTools, t)
		}
	}

	if len(agent.MCPServers) > 0 {
		for _, ms := range agent.MCPServers {
			if ms.Type == "stdio" {
				slog.Warn("stdio MCP server not supported at runtime, skipping", "server", ms.Name, "session_id", sessionID)
				continue
			}
			if ms.Type == "http" {
				allTools = append(allTools,
					&ToolDef{
						Type: "function",
						Function: struct {
							Name        string `json:"name"`
							Description string `json:"description"`
							Parameters  any    `json:"parameters"`
						}{
							Name:        ms.Name + "_list_tools",
							Description: fmt.Sprintf("List available tools from MCP server %s", ms.Name),
							Parameters:  "{}",
						},
						Handler: makeMCPToolListHandlerHTTP(ms.Command),
					},
					&ToolDef{
						Type: "function",
						Function: struct {
							Name        string `json:"name"`
							Description string `json:"description"`
							Parameters  any    `json:"parameters"`
						}{
							Name:        ms.Name + "_call_tool",
							Description: fmt.Sprintf("Call a tool on MCP server %s", ms.Name),
							Parameters:  `{"type":"object","properties":{"tool":{"type":"string"},"arguments":{"type":"object"}},"required":["tool"]}`,
						},
						Handler: makeMCPToolCallHandlerHTTP(ms.Command),
					},
				)
			}
		}
	}

	toolsList := make([]OpenAITool, len(allTools))
	for i, t := range allTools {
		oat, err := t.ToOpenAITool()
		if err != nil {
			return "", "", nil, events, nil, fmt.Errorf("convert tool: %w", err)
		}
		toolsList[i] = oat
	}

	toolLookup := make(map[string]*ToolDef, len(allTools))
	for _, t := range allTools {
		toolLookup[t.Function.Name] = t
	}

	tokBrk := &TokenBreakdown{}
	if modelName == "" {
		modelName = "gpt-4"
	}
	sysContent := systemPrompt + notebookCtx + skillCatalogStr
	sysTokens := e.tokenCounter.CountText(sysContent, modelName)
	chatMsgsForCount := make([]ChatMessage, 0)
	for _, m := range messages {
		msg := ChatMessage{Role: m.Role, Content: m.Content}
		if m.ReasoningContent != "" {
			msg.ReasoningContent = m.ReasoningContent
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				argsStr := ""
				if b, err := json.Marshal(tc.Arguments); err == nil {
					argsStr = string(b)
				}
				msg.ToolCalls = append(msg.ToolCalls, ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Name,
						Arguments: argsStr,
					},
				})
			}
		}
		if m.ToolCallID != nil {
			msg.ToolCallID = *m.ToolCallID
		}
		chatMsgsForCount = append(chatMsgsForCount, msg)
	}
	historyTokens := e.tokenCounter.CountMessages(chatMsgsForCount, modelName)
	userTokens := e.tokenCounter.CountText(effectiveMessage, modelName)
	var skillTokens int
	if skillOverridePrompt != "" {
		skillTokens = e.tokenCounter.CountText("# Active Skill\n\n"+skillOverridePrompt, modelName)
	}
	toolDefTokens := e.tokenCounter.CountToolDefs(toolsList, modelName)

	maxTurns := 90
	if agent.MaxTurns != nil && *agent.MaxTurns > 0 {
		maxTurns = *agent.MaxTurns
	}
	var allToolCalls []models.ToolCall
	modelCalls := 0
	apiInputTotal := 0
	var estimatedToolCalls, estimatedToolResults int

	var llmErrorCount int
	for turn := 0; turn < maxTurns; turn++ {
		turnStart := time.Now()
		slog.Debug("engine: calling LLM", "session_id", sessionID, "turn", turn, "msgs", len(chatMsgs), "tools", len(toolsList))
		chatMsgs = sanitizeChatMessages(chatMsgs)
		llmStart := time.Now()
		resp, err := llmClient.Chat(ctx, chatMsgs, toolsList, masterKey)
		if err != nil {
			slog.Error("engine: LLM call failed", "session_id", sessionID, "turn", turn, "error", err, "elapsed_ms", time.Since(turnStart).Milliseconds())
			llmErrorCount++
			if llmErrorCount >= 3 {
				return "", "", nil, events, tokBrk, fmt.Errorf("llm call failed after 3 retries: %w", err)
			}
			continue
		}
		llmDuration := int(time.Since(llmStart).Milliseconds())
		tokBrk.DurationMs = llmDuration
		llmElapsed := time.Since(turnStart).Milliseconds()
		llmErrorCount = 0

		if len(resp.Choices) == 0 {
			slog.Error("engine: no choices in LLM response", "session_id", sessionID)
			continue
		}

		modelCalls++
		apiInputTotal += resp.Usage.PromptTokens
		tokBrk.Output += resp.Usage.CompletionTokens
		if resp.Usage.CompletionTokensDetails != nil {
			tokBrk.Reasoning += resp.Usage.CompletionTokensDetails.ReasoningTokens
		}
		if resp.Usage.PromptTokensDetails != nil {
			tokBrk.CacheRead += resp.Usage.PromptTokensDetails.CachedTokens
		}

		if onEvent != nil {
			onEvent(EngineEvent{
				Type: "token_update",
				Tokens: &TokenBreakdown{
					Input:           apiInputTotal,
					Output:          tokBrk.Output,
					Reasoning:       tokBrk.Reasoning,
					CacheRead:       tokBrk.CacheRead,
					ModelCalls:      modelCalls,
					SystemPrompt:    sysTokens,
					SkillOverride:   skillTokens,
					History:         historyTokens,
					UserMessage:     userTokens,
					ToolDefinitions: toolDefTokens,
					ToolCalls:       estimatedToolCalls,
					ToolResults:     estimatedToolResults,
				},
			})
		}

		// Auto-compact if approaching context window limit
		if compactionThreshold > 0 && apiInputTotal > contextWindow*compactionThreshold/100 && len(chatMsgs) > 10 {
			chatMsgs = e.compactChatHistory(ctx, llmClient, chatMsgs, masterKey, sessionID)
			slog.Info("context compaction triggered", "session_id", sessionID, "tokens", apiInputTotal, "context_window", contextWindow)
		}

		choice := resp.Choices[0]
		text := choice.Message.Content
		toolCalls := choice.Message.ToolCalls
		reasoningContent := choice.Message.ReasoningContent

		if len(toolCalls) == 0 {
			if text == "" {
				text = "I wasn't able to generate a response. Could you rephrase your request?"
			}
			slog.Debug("engine: final response", "session_id", sessionID, "text_len", len(text), "reasoning_len", len(reasoningContent), "tool_calls_total", len(allToolCalls))
			if onReasoning != nil && reasoningContent != "" {
				chunk(reasoningContent, 10, func(s string) {
					onReasoning(s)
					time.Sleep(12 * time.Millisecond)
				})
			}
			if onToken != nil {
				chunk(text, 4, func(s string) {
					onToken(s)
					time.Sleep(18 * time.Millisecond)
				})
			}
			msgID := uuid.New().String()
			tokBrk.Input = apiInputTotal
			tokBrk.ModelCalls = modelCalls
			tokBrk.SystemPrompt = sysTokens
			tokBrk.History = historyTokens
			tokBrk.UserMessage = userTokens
			tokBrk.ToolDefinitions = toolDefTokens
			tokBrk.SkillOverride = skillTokens
			tokBrk.ToolCalls = estimatedToolCalls
			tokBrk.ToolResults = estimatedToolResults
			agentMsg := &models.AgentMessage{
				ID:               msgID,
				SessionID:        sessionID,
				Role:             "assistant",
				Content:          text,
				ReasoningContent: reasoningContent,
				TokensInput:      apiInputTotal,
				TokensOutput:     tokBrk.Output,
				TokensReasoning:  tokBrk.Reasoning,
				ModelCalls:       modelCalls,
				DurationMs:       llmDuration,
				CreatedAt:        time.Now(),
			}
			e.session.AppendMessage(ctx, agentMsg)
			return text, reasoningContent, allToolCalls, events, tokBrk, nil
		} else {
			slog.Debug("engine: tool calls in response", "session_id", sessionID, "turn", turn, "num_tool_calls", len(toolCalls), "text_len", len(text))
		}

		chatMsgs = append(chatMsgs, ChatMessage{
			Role:             "assistant",
			Content:          text,
			ToolCalls:        toolCalls,
			ReasoningContent: reasoningContent,
		})

		assistantMsgID := uuid.New().String()
		msgToolCalls := make([]models.ToolCall, 0, len(toolCalls))
		for _, tc := range toolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			msgToolCalls = append(msgToolCalls, models.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			})
		}
		e.session.AppendMessage(context.Background(), &models.AgentMessage{
			ID:               assistantMsgID,
			SessionID:        sessionID,
			Role:             "assistant",
			Content:          text,
			ToolCalls:        msgToolCalls,
			ReasoningContent: reasoningContent,
			TokensInput:      apiInputTotal,
			TokensOutput:     tokBrk.Output,
			TokensReasoning:  tokBrk.Reasoning,
			ModelCalls:       modelCalls,
			DurationMs:       llmDuration,
			CreatedAt:        time.Now(),
		})

		if onReasoning != nil && reasoningContent != "" {
			chunk(reasoningContent, 8, func(s string) {
				onReasoning(s)
				time.Sleep(12 * time.Millisecond)
			})
		}

		firstTool := true
		for _, tc := range toolCalls {
			if onToolCall != nil {
				r := ""
				if firstTool {
					r = reasoningContent
					firstTool = false
				}
				onToolCall(tc.Function.Name, tc.ID, tc.Function.Arguments, r, llmDuration)
			}
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			allToolCalls = append(allToolCalls, models.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			})
			estimatedToolCalls += e.tokenCounter.CountText(tc.Function.Arguments, modelName)

			toolDef, ok := toolLookup[tc.Function.Name]
			if !ok {
				toolDef, ok = e.registry.Get(tc.Function.Name)
			}
			if !ok {
				resultStr := fmt.Sprintf("unknown tool: %s", tc.Function.Name)
				chatMsgs = append(chatMsgs, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: resultStr})
			estimatedToolResults += e.tokenCounter.CountText(resultStr, modelName)
			e.session.AppendMessage(context.Background(), &models.AgentMessage{
				ID:         uuid.New().String(),
				SessionID:  sessionID,
				Role:       "tool",
				ToolCallID: &tc.ID,
				Content:    resultStr,
				CreatedAt:  time.Now(),
			})
			if onToolResult != nil {
				onToolResult(tc.Function.Name, tc.Function.Arguments, resultStr, "", 0)
			}
			continue
		}

		toolCtx := &ToolContext{
				Context:       ctx,
				UserID:        session.UserID,
				OrgID:         agent.OrgID,
				OrgRole:       orgRole,
				NotebookID:    session.NotebookID,
				SessionID:     sessionID,
				DB:            e.pool,
				TurnCount:     turn,
				Events:        &events,
				MasterKey:     masterKey,
				OnEvent:       onEvent,
				BroadcastFunc: e.BroadcastFunc,
			}

			if toolDef.ConfirmRequired && onEvent != nil {
				ch := make(chan ToolConfirmResult, 1)
				e.SetToolConfirm(sessionID, ch)
				eventArgs := tc.Function.Arguments
				var currentSource string
				if tc.Function.Name == "update_cell" {
					var args struct {
						CellID string `json:"cell_id"`
					}
					if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil && args.CellID != "" {
						e.pool.QueryRow(ctx, `SELECT source FROM cells WHERE id = $1`, args.CellID).Scan(&currentSource)
					}
				}
				onEvent(EngineEvent{Type: "tool_confirm_required", ToolName: tc.Function.Name, ToolArgs: eventArgs, Source: currentSource})
				select {
				case res := <-ch:
					if !res.Approved {
						resultStr := fmt.Sprintf("Tool call '%s' was denied by user", tc.Function.Name)
						chatMsgs = append(chatMsgs, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: resultStr})
						estimatedToolResults += e.tokenCounter.CountText(resultStr, modelName)
						e.session.AppendMessage(context.Background(), &models.AgentMessage{
							ID:         uuid.New().String(),
							SessionID:  sessionID,
							Role:       "tool",
							ToolCallID: &tc.ID,
							Content:    resultStr,
							CreatedAt:  time.Now(),
						})
						if onToolResult != nil {
							onToolResult(tc.Function.Name, tc.Function.Arguments, resultStr, "", 0)
						}
						continue
					}
				case <-ctx.Done():
					resultStr := fmt.Sprintf("Tool call '%s' timed out waiting for approval", tc.Function.Name)
					chatMsgs = append(chatMsgs, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: resultStr})
					estimatedToolResults += e.tokenCounter.CountText(resultStr, modelName)
					e.session.AppendMessage(context.Background(), &models.AgentMessage{
						ID:         uuid.New().String(),
						SessionID:  sessionID,
						Role:       "tool",
						ToolCallID: &tc.ID,
						Content:    resultStr,
						CreatedAt:  time.Now(),
					})
					if onToolResult != nil {
						onToolResult(tc.Function.Name, tc.Function.Arguments, resultStr, "timeout", 0)
					}
					continue
				}
			}

			toolStart := time.Now()
			result, err := toolDef.Handler([]byte(tc.Function.Arguments), toolCtx)
			toolDurationMs := int(time.Since(toolStart).Milliseconds())
			if err != nil {
				resultStr := fmt.Sprintf("error: %s", err.Error())
				chatMsgs = append(chatMsgs, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: resultStr})
				estimatedToolResults += e.tokenCounter.CountText(resultStr, modelName)
				e.session.AppendMessage(context.Background(), &models.AgentMessage{
					ID:         uuid.New().String(),
					SessionID:  sessionID,
					Role:       "tool",
					ToolCallID: &tc.ID,
					Content:    resultStr,
					DurationMs: toolDurationMs,
					CreatedAt:  time.Now(),
				})
				if onToolResult != nil {
					onToolResult(tc.Function.Name, tc.Function.Arguments, "", err.Error(), toolDurationMs)
				}
			} else {
				resultJSON, _ := json.Marshal(result)
				resultStr := string(resultJSON)
				chatMsgs = append(chatMsgs, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: resultStr})
				estimatedToolResults += e.tokenCounter.CountText(resultStr, modelName)
				e.session.AppendMessage(context.Background(), &models.AgentMessage{
					ID:         uuid.New().String(),
					SessionID:  sessionID,
					Role:       "tool",
					ToolCallID: &tc.ID,
					Content:    resultStr,
					DurationMs: toolDurationMs,
					CreatedAt:  time.Now(),
				})
				if onToolResult != nil {
					onToolResult(tc.Function.Name, tc.Function.Arguments, resultStr, "", toolDurationMs)
				}
			}
		}

		slog.Debug("engine: turn complete", "session_id", sessionID, "turn", turn, "llm_ms", llmElapsed, "total_ms", time.Since(turnStart).Milliseconds())
	}

	tokBrk.Input = apiInputTotal
	tokBrk.ModelCalls = modelCalls
	tokBrk.SystemPrompt = sysTokens
	tokBrk.History = historyTokens
	tokBrk.UserMessage = userTokens
	tokBrk.ToolDefinitions = toolDefTokens
	tokBrk.SkillOverride = skillTokens
	tokBrk.ToolCalls = estimatedToolCalls
	tokBrk.ToolResults = estimatedToolResults
	return "", "", allToolCalls, events, tokBrk, fmt.Errorf("max turns reached")
}

func (e *Engine) resolveToolDef(t *models.Tool) (*ToolDef, error) {
	switch t.Type {
	case models.ToolTypeBuiltin:
		handlerName, _ := t.Config["handler_name"].(string)
		if handlerName == "" {
			return nil, fmt.Errorf("builtin tool missing handler_name")
		}
		def, ok := e.registry.Get(handlerName)
		if !ok {
			return nil, fmt.Errorf("builtin handler not found: %s", handlerName)
		}
		return def, nil
	case models.ToolTypeWebhook:
		return makeWebhookToolDef(t, e.toolAllowedDomains)
	case models.ToolTypeSQLQuery:
		return makeSQLQueryToolDef(t, e.pool)
	default:
		return nil, fmt.Errorf("unknown tool type: %s", t.Type)
	}
}

func (e *Engine) checkToolUsePermission(ctx context.Context, userID, orgID, orgRole, toolID string) (bool, error) {
	if orgRole == "admin" {
		return true, nil
	}
	var exists bool
	err := e.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM acl_entries
			WHERE resource_type = 'tool' AND resource_id = $1 AND org_id = $2
			AND (
				(subject_type = 'user' AND subject_id = $3)
				OR (subject_type = 'org_role' AND subject_id = $4)
				OR (subject_type = 'org_role' AND subject_id = 'everyone')
			)
			AND 'use' = ANY(actions)
		)
	`, toolID, orgID, userID, orgRole).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check tool use permission: %w", err)
	}
	return exists, nil
}

func (e *Engine) GetRegistry() *ToolRegistry {
	return e.registry
}

func (e *Engine) SessionStore() *SessionStore {
	return e.session
}

func (e *Engine) SetLLMClient(llm *LLMClient) {
	e.llm = llm
}

// defaultSubagentLLM resolves the model config for subagent LLM calls.
// It looks up the parent agent's model config and creates an LLM client,
// falling back to the engine's default LLM if no config is found.
func (e *Engine) defaultSubagentLLM(ctx context.Context, pool *pgxpool.Pool, agentID string, masterKey []byte) *LLMClient {
	var mc models.ModelConfig
	var defaultParams []byte
	err := pool.QueryRow(ctx, `
		SELECT mc.id, mc.org_id, mc.name, mc.provider, mc.base_url, mc.model, mc.api_key_encrypted, mc.default_params, mc.context_window
		FROM model_configs mc
		JOIN agents a ON a.model_config_id = mc.id OR a.subagent_model_config_id = mc.id
		WHERE a.id = $1
		LIMIT 1
	`, agentID).Scan(&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &mc.APIKeyEncrypted, &defaultParams, &mc.ContextWindow)
	if err != nil {
		return e.llm
	}
	if defaultParams != nil {
		json.Unmarshal(defaultParams, &mc.DefaultParams)
	}
	return NewLLMClient(mc.BaseURL, mc.Model, mc.APIKeyEncrypted, mc.DefaultParams)
}

func (e *Engine) SetToolAllowedDomains(domains []string) {
	e.toolAllowedDomains = domains
}

func (e *Engine) PublishSessionEvent(sessionID string, msg any) {
	e.streams.Publish(sessionID, msg)
}

func (e *Engine) SubscribeSession(sessionID string, bufSize int, skipBuffer bool) (chan any, func()) {
	return e.streams.Subscribe(sessionID, bufSize, skipBuffer)
}

func (e *Engine) SetFrontendURL(u string) {
	e.frontendURL = u
}

func (e *Engine) SetStore(store storage.Storage) {
	e.store = store
}

func (e *Engine) FetchImageDataURIs(ctx context.Context, imageIDs []string) ([]string, error) {
	if len(imageIDs) == 0 || e.store == nil {
		return nil, nil
	}

	dataURIs := make([]string, 0, len(imageIDs))
	for _, id := range imageIDs {
		var mimeType string
		err := e.pool.QueryRow(ctx, `SELECT mime_type FROM session_attachments WHERE id = $1`, id).Scan(&mimeType)
		if err != nil {
			slog.Warn("failed to get attachment mime type", "id", id, "error", err)
			continue
		}

		rc, err := e.store.Get(id)
		if err != nil {
			slog.Warn("failed to get attachment from storage", "id", id, "error", err)
			continue
		}

		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			slog.Warn("failed to read attachment", "id", id, "error", err)
			continue
		}

		encoded := base64.StdEncoding.EncodeToString(data)
		dataURIs = append(dataURIs, fmt.Sprintf("data:%s;base64,%s", mimeType, encoded))
	}
	return dataURIs, nil
}



func (e *Engine) HandleSlashCommand(ctx context.Context, sessionID string, command string, orgID string, masterKey []byte) (any, error) {
	cmd := strings.TrimSpace(command)
	switch cmd {
	case "skills":
		return e.listSkills(ctx, orgID)
	case "agents":
		return e.listAgents(ctx, orgID)
	case "new":
		if count, err := e.session.GetMessageCount(ctx, sessionID); err == nil && count == 0 {
			e.session.DeleteSession(ctx, sessionID)
		}
		return map[string]string{"session_id": sessionID}, nil
	case "summarize":
		return e.summarizeAndNewSession(ctx, sessionID, masterKey)
	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

func (e *Engine) listSkills(ctx context.Context, orgID string) (map[string]any, error) {
	rows, err := e.pool.Query(ctx, `SELECT id, name, description FROM skills WHERE org_id = $1 LIMIT 50`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []map[string]string
	for rows.Next() {
		var id, name, desc string
		if err := rows.Scan(&id, &name, &desc); err != nil {
			return nil, err
		}
		skills = append(skills, map[string]string{"id": id, "name": name, "description": desc})
	}
	return map[string]any{"skills": skills}, nil
}

func (e *Engine) listAgents(ctx context.Context, orgID string) (map[string]any, error) {
	rows, err := e.pool.Query(ctx, `SELECT id, name, description FROM agents WHERE org_id = $1 LIMIT 50`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []map[string]string
	for rows.Next() {
		var id, name, desc string
		if err := rows.Scan(&id, &name, &desc); err != nil {
			return nil, err
		}
		agents = append(agents, map[string]string{"id": id, "name": name, "description": desc})
	}
	return map[string]any{"agents": agents}, nil
}

func (e *Engine) summarizeSession(ctx context.Context, sessionID string, masterKey []byte) (map[string]any, error) {
	messages, err := e.session.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if len(messages) == 0 {
		return map[string]any{"summary": "No messages in session"}, nil
	}

	recentMessages := messages
	if len(messages) > 20 {
		recentMessages = messages[len(messages)-20:]
	}

	summarizePrompt := "Summarize the following conversation concisely, preserving key information, decisions, and context:\n\n"
	for _, m := range recentMessages {
		summarizePrompt += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
	}

	// Initialize LLM client the same way as ProcessMessage
	session, err := e.session.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	var agent struct {
		ModelConfigID *string
	}
	err = e.pool.QueryRow(ctx, `SELECT model_config_id FROM agents WHERE id = $1`, session.AgentID).Scan(&agent.ModelConfigID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}

	llmClient := e.llm
	if agent.ModelConfigID != nil && *agent.ModelConfigID != "" {
		var mc models.ModelConfig
		var defaultParams []byte
		err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, folder_id, created_by, created_at, updated_at FROM model_configs WHERE id = $1`, *agent.ModelConfigID).Scan(
			&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &mc.APIKeyEncrypted, &defaultParams, &mc.ContextWindow, &mc.FolderID, &mc.CreatedBy, &mc.CreatedAt, &mc.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("get model config: %w", err)
		}
		if defaultParams != nil {
			json.Unmarshal(defaultParams, &mc.DefaultParams)
		}
		llmClient = NewLLMClient(mc.BaseURL, mc.Model, mc.APIKeyEncrypted, mc.DefaultParams)
	}

	if llmClient == nil {
		return map[string]any{"summary": "No LLM client configured"}, nil
	}

	resp, err := llmClient.Chat(ctx, []ChatMessage{{Role: "user", Content: summarizePrompt}}, nil, masterKey)
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) > 0 {
		return map[string]any{"summary": resp.Choices[0].Message.Content}, nil
	}
	return map[string]any{"summary": "Could not generate summary"}, nil
}

func (e *Engine) summarizeAndNewSession(ctx context.Context, sessionID string, masterKey []byte) (map[string]any, error) {
	// 1. Get the summary
	result, err := e.summarizeSession(ctx, sessionID, masterKey)
	if err != nil {
		return nil, err
	}
	summary, _ := result["summary"].(string)
	if summary == "" {
		return result, nil
	}

	// 2. Get old session details
	oldSession, err := e.session.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get old session: %w", err)
	}

	// 3. Create a new session
	newSession, err := e.session.CreateSession(ctx, oldSession.AgentID, oldSession.NotebookID, oldSession.UserID, oldSession.MaxTurns, nil)
	if err != nil {
		return nil, fmt.Errorf("create new session: %w", err)
	}

	// 4. Store the summary as a system message in the new session
	sysMsg := &models.AgentMessage{
		ID:        uuid.New().String(),
		SessionID: newSession.ID,
		Role:      "user",
		Content:   "Context from previous session summary:\n\n" + summary,
		CreatedAt: time.Now(),
	}
	if err := e.session.AppendMessage(ctx, sysMsg); err != nil {
		return nil, fmt.Errorf("store summary: %w", err)
	}

	return map[string]any{
		"session_id": newSession.ID,
		"summary":    summary,
	}, nil
}

func (e *Engine) generateSessionTitle(ctx context.Context, sessionID string, masterKey []byte) error {
	messages, err := e.session.GetMessagesWithLimit(ctx, sessionID, 5)
	if err != nil {
		return fmt.Errorf("get messages: %w", err)
	}

	if len(messages) < 2 {
		return nil
	}

	prompt := "Generate a concise title (max 50 characters) for this conversation. Only return the title, nothing else:\n\n"
	for _, m := range messages {
		if m.Role == "user" && m.Content != "" {
			prompt += fmt.Sprintf("User: %s\n", truncateStr(m.Content, 200))
		}
	}

	session, err := e.session.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	llmClient := e.llm
	if session.AgentID != "" {
		var agentModelConfigID *string
		err = e.pool.QueryRow(ctx, `SELECT model_config_id FROM agents WHERE id = $1`, session.AgentID).Scan(&agentModelConfigID)
		if err == nil && agentModelConfigID != nil && *agentModelConfigID != "" {
			var mc models.ModelConfig
			var defaultParams []byte
			err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, folder_id, created_by, created_at, updated_at FROM model_configs WHERE id = $1`, *agentModelConfigID).Scan(
				&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &mc.APIKeyEncrypted, &defaultParams, &mc.ContextWindow, &mc.FolderID, &mc.CreatedBy, &mc.CreatedAt, &mc.UpdatedAt)
			if err == nil {
				if defaultParams != nil {
					json.Unmarshal(defaultParams, &mc.DefaultParams)
				}
				llmClient = NewLLMClient(mc.BaseURL, mc.Model, mc.APIKeyEncrypted, mc.DefaultParams)
			}
		}
	}

	if llmClient == nil {
		return nil
	}

	resp, err := llmClient.Chat(ctx, []ChatMessage{{Role: "user", Content: prompt}}, nil, masterKey)
	if err != nil {
		return fmt.Errorf("llm chat: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil
	}

	title := strings.TrimSpace(resp.Choices[0].Message.Content)
	if title == "" {
		return nil
	}

	runes := []rune(title)
	if len(runes) > 50 {
		title = string(runes[:50])
	}

	return e.session.UpdateTitle(ctx, sessionID, &title)
}

func truncateStr(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

func chunk(s string, size int, fn func(string)) {
	runes := []rune(s)
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		fn(string(runes[i:end]))
	}
}

// buildNotebookContext fetches notebook and connector info to build a context string
// that tells the agent what it's working with.
func (e *Engine) buildNotebookContext(ctx context.Context, notebookID string) string {
	if notebookID == "" {
		return "No notebook selected."
	}

	var title string
	var connectorID *string
	err := e.pool.QueryRow(ctx,
		`SELECT title, connector_id FROM notebooks WHERE id = $1`, notebookID).
		Scan(&title, &connectorID)
	if err != nil {
		return fmt.Sprintf("Current notebook: %s", notebookID)
	}

	base := e.frontendURL
	result := fmt.Sprintf("Current notebook: %s (title: %q)", notebookID, title)
	if base != "" {
		result = fmt.Sprintf("Current notebook: %s (title: %q, link: %s/notebooks/%s)", notebookID, title, base, notebookID)
	}

	if connectorID != nil && *connectorID != "" {
		var connName, connType string
		err := e.pool.QueryRow(ctx,
			`SELECT name, type FROM connectors WHERE id = $1`, *connectorID).
			Scan(&connName, &connType)
		if err == nil {
			result += fmt.Sprintf("\nConnector: %q (type: %s, id: %s)", connName, connType, *connectorID)
			result += "\nNotebook cells: type 'code' with language 'sql' for database queries, type 'text' with language 'markdown' for documentation."
			result += "\nCharts: Use create_chart to turn a cell's table output into a chart. Types: bar, line, area, scatter, pie, donut, timeline, hierarchy_tree, big_number, map, sankey, funnel, heatmap, histogram. For map charts, use lat_column and lon_column parameters instead of x_column/y_columns."
			result += "\n  Common params (all types): title, show_labels, show_legend, show_grid, skip_empty, series_colors (dict of series name to hex color)"
			result += "\n  Bar: x_column (categories), y_columns (values). Use bar_mode to set layout: grouped (default), stacked (stacked totals), or horizontal (left-to-right bars). Also: group_by, bar_width (% string), bar_gap (% string), data_zoom."
			result += "\n  Line: x_column (categories), y_columns (values). Also: smooth (boolean), connect_nulls, data_zoom."
			result += "\n  Area: x_column (categories), y_columns (values). Use area_mode to set layout: area (overlapping) or stacked (stacked areas). Also: group_by, smooth, connect_nulls, data_zoom."
			result += "\n  Scatter: x_column (numeric), y_columns (values). Also: group_by (split into series by column), color_column (maps 3rd dim to color gradient), size_column (bubble size), data_zoom (always enabled)."
			result += "\n  Pie/donut: x_column (slice name) or label_column (slice name), y_columns (metric value). Also: rose_type (radius|area), start_angle (0-360), pad_angle (gap between slices)."
			result += "\n  Timeline: time_column, end_time_column (optional for ranges), label_column, group_by (swim lanes). Also: show_connectors, show_time_deltas, max_label_length."
			result += "\n  Hierarchy tree: id_column, parent_id_column, label_column, metric_columns, layout (top-down|left-to-right)."
			result += "\n  Big number: value_column, label (display text), prefix, suffix, decimal_places."
			result += "\n  Map: x_column=longitude, y_columns[0]=latitude, y_columns[1]=value (optional), label_column."
			result += "\n  Sankey: x_column=source, y_columns[0]=target, y_columns[1]=value. Also: node_align (justify|left|right), node_width, node_gap."
			result += "\n  Funnel: category_column (stage labels) or x_column, value_column (stage values) or y_columns[0]. Also: funnel_sort (descending|ascending|none), suffix (unit)."
			result += "\n  Heatmap: x_column (columns), y_axis_column (rows), value_column (intensity) or y_columns[0]. Best for cross-tabulation/matrix data (e.g. event types × hour of day)."
			result += "\n  Histogram: value_column (numeric column to bin). Also: bin_count (number of bins, default auto). Creates frequency distribution of a numeric column."
			result += "\nUse update_chart to modify an existing chart's config. The frontend renders automatically from saved config."
		}
	}

	// Add cell count information
	var cellCount int
	var codeCellCount int
	e.pool.QueryRow(ctx, `SELECT COUNT(*) FROM cells WHERE notebook_id = $1`, notebookID).Scan(&cellCount)
	e.pool.QueryRow(ctx, `SELECT COUNT(*) FROM cells WHERE notebook_id = $1 AND type = 'code'`, notebookID).Scan(&codeCellCount)
	if cellCount > 0 {
		result += fmt.Sprintf("\nCells: %d total (code: %d)", cellCount, codeCellCount)
		result += "\nUse get_notebook_context tool to read full cell contents."
	}

	// Add subagent capability mention
	result += "\n\nYou have the spawn_subagents tool to run multiple independent tasks in parallel via separate AI sub-agents. Use it when a request contains several distinct subtasks that don't depend on each other's results. For example: exploring multiple schemas at once, generating several unrelated queries, or researching different topics simultaneously."

	// Add dashboard tools mention
	result += "\n\nDashboard tools: create_dashboard, get_dashboard (see widgets), create_dashboard_widget (add cell as widget), update_dashboard_widget (reposition), delete_dashboard_widget, update_dashboard (rename/grid), delete_dashboard, list_dashboards, share_dashboard. To build a dashboard: create it, then list_cells on the notebook, check each cell's chart config (shown in list_cells output as 'chart'), then create_dashboard_widget for each cell. Copy the notebook_id when adding widgets."

	// Add resource link patterns
	result += "\n\nResource link patterns (use these to provide clickable links to the user):"
	result += "\n- Notebook: " + asLink(base, "/notebooks/{id}")
	result += "\n- Dashboard editor: " + asLink(base, "/dashboards/{id}")
	result += "\n- Dashboard view: " + asLink(base, "/dashboards/{id}/view")
	result += "\n- Folder: " + asLink(base, "/?folder={id}")
	result += "\n- Connectors list: " + asLink(base, "/connectors")
	result += "\nWhen a user asks for a link, provide it as a markdown link: [title](full_url). Use the resource IDs from tool results."

	return result
}

// asLink formats a URL path as a markdown link example for the agent context.
func asLink(base, path string) string {
	example := path
	// Replace the first {param} with a concrete example if base is known
	if base != "" {
		return fmt.Sprintf("`%s%s`", base, path)
	}
	return fmt.Sprintf("`%s` (relative path, prefix with app hostname)", example)
}
