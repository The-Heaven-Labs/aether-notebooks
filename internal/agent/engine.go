package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Engine struct {
	registry            *ToolRegistry
	session             *SessionStore
	llm                 *LLMClient
	pool                *pgxpool.Pool
	mu                  sync.Mutex
	rateLimiter         *RateLimiter
	BroadcastFunc       func(notebookID string, msg any)
	toolAllowedDomains  []string
}

func NewEngine(ctx context.Context, pool *pgxpool.Pool) *Engine {
	engine := &Engine{
		registry:    NewToolRegistry(),
		session:     NewSessionStore(pool),
		pool:        pool,
		rateLimiter: NewRateLimiter(pool),
	}

	RegisterNotebookTools(engine.registry, pool)
	RegisterAgentTools(engine.registry, pool, engine)
	RegisterPlatformTools(engine.registry, pool)
	RegisterChartTools(engine.registry, pool)

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

func (e *Engine) ProcessMessage(ctx context.Context, sessionID string, userMessage string, tools []*ToolDef, masterKey []byte, onToken func(string), onReasoning func(string), onToolCall func(string, string, string), onToolResult func(string, string, string, string), onEvent func(EngineEvent)) (string, string, []models.ToolCall, []EngineEvent, error) {
	var events []EngineEvent
	slog.Debug("engine: ProcessMessage start", "session_id", sessionID, "msg_len", len(userMessage))
	session, err := e.session.GetSession(ctx, sessionID)
	if err != nil {
		slog.Error("engine: get session failed", "session_id", sessionID, "error", err)
		return "", "", nil, events, fmt.Errorf("get session: %w", err)
	}

	var agent models.Agent
	var systemPrompt string
	var skillIDs []byte
	var toolIDs []byte
	err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, description, model_config_id, subagent_model_config_id, system_prompt, array_to_json(skill_ids)::text, array_to_json(tool_ids)::text, folder_id, max_turns, created_by, created_at, updated_at FROM agents WHERE id = $1`, session.AgentID).Scan(
		&agent.ID, &agent.OrgID, &agent.Name, &agent.Description, &agent.ModelConfigID, &agent.SubagentModelConfigID, &systemPrompt, &skillIDs, &toolIDs, &agent.FolderID, &agent.MaxTurns, &agent.CreatedBy, &agent.CreatedAt, &agent.UpdatedAt)
	if err != nil {
		return "", "", nil, events, fmt.Errorf("get agent: %w", err)
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

	// Load agent tools from tools table
	agentTools := make([]*ToolDef, 0)
	if len(agent.ToolIDs) > 0 {
		tRows, err := e.pool.Query(ctx, `
			SELECT id, org_id, name, description, type, schema, config
			FROM tools WHERE id = ANY($1)`, agent.ToolIDs)
		if err == nil {
			for tRows.Next() {
				var t models.Tool
				var schema, config []byte
				if err := tRows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &t.Type, &schema, &config); err != nil {
					continue
				}
				if schema != nil {
					json.Unmarshal(schema, &t.Schema)
				}
				if config != nil {
					json.Unmarshal(config, &t.Config)
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

	ok, err := e.rateLimiter.CheckAndUpdateTokens(ctx, sessionID, 0, 0)
	if err != nil {
		slog.Error("rate limit check failed", "session_id", sessionID, "error", err)
		return "", "", nil, events, fmt.Errorf("rate limit check: %w", err)
	}
	if !ok {
		slog.Warn("rate limit exceeded", "session_id", sessionID)
		return "", "", nil, events, fmt.Errorf("rate limit exceeded: session has reached max turns or tokens")
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
	if agent.ModelConfigID != nil && *agent.ModelConfigID != "" {
		slog.Debug("engine: using agent model config", "session_id", sessionID, "model_config_id", *agent.ModelConfigID)
		var mc models.ModelConfig
		var defaultParams []byte
		err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, folder_id, created_by, created_at, updated_at FROM model_configs WHERE id = $1`, *agent.ModelConfigID).Scan(
			&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &mc.APIKeyEncrypted, &defaultParams, &mc.ContextWindow, &mc.FolderID, &mc.CreatedBy, &mc.CreatedAt, &mc.UpdatedAt)
		if err != nil {
			return "", "", nil, events, fmt.Errorf("get model config: %w", err)
		}
		if defaultParams != nil {
			json.Unmarshal(defaultParams, &mc.DefaultParams)
		}
		llmClient = NewLLMClient(mc.BaseURL, mc.Model, mc.APIKeyEncrypted)
	} else {
		slog.Warn("engine: no model config on agent, using default LLM client", "session_id", sessionID, "default_llm_nil", e.llm == nil)
	}

	if llmClient == nil {
		return "", "", nil, events, fmt.Errorf("no LLM client available: assign a model config to this agent in the Agents page")
	}

	var orgRole string
	err = e.pool.QueryRow(ctx, `SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, agent.OrgID, session.UserID).Scan(&orgRole)
	if err != nil {
		orgRole = "editor"
	}

	messages, err := e.session.GetMessages(ctx, sessionID)
	if err != nil {
		return "", "", nil, events, fmt.Errorf("get messages: %w", err)
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
			return "", "", nil, events, fmt.Errorf("skill '%s' not found", skillName)
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
	notebookCtx := e.buildNotebookContext(session.NotebookID)
	if systemPrompt != "" {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: systemPrompt + notebookCtx + skillCatalogStr})
	} else {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: notebookCtx + skillCatalogStr})
	}
	for _, m := range messages {
		msg := ChatMessage{Role: m.Role, Content: m.Content}
		if m.ReasoningContent != "" {
			msg.ReasoningContent = m.ReasoningContent
		}
		chatMsgs = append(chatMsgs, msg)
	}
	if skillOverridePrompt != "" {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: "# Active Skill\n\n" + skillOverridePrompt})
	}
	chatMsgs = append(chatMsgs, ChatMessage{Role: "user", Content: effectiveMessage})

	userMsgID := uuid.New().String()
	e.session.AppendMessage(ctx, &models.AgentMessage{
		ID:        userMsgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   effectiveMessage,
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
	allTools = append(allTools, agentTools...)
	allTools = append(allTools, tools...)

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
			return "", "", nil, events, fmt.Errorf("convert tool: %w", err)
		}
		toolsList[i] = oat
	}

	toolLookup := make(map[string]*ToolDef, len(allTools))
	for _, t := range allTools {
		toolLookup[t.Function.Name] = t
	}

	maxTurns := 90
	if agent.MaxTurns != nil && *agent.MaxTurns > 0 {
		maxTurns = *agent.MaxTurns
	}
	var allToolCalls []models.ToolCall
	totalTokensInput := 0
	totalTokensOutput := 0
	modelCalls := 0

	for turn := 0; turn < maxTurns; turn++ {
		slog.Debug("engine: calling LLM", "session_id", sessionID, "turn", turn, "msgs", len(chatMsgs), "tools", len(toolsList))
		resp, err := llmClient.Chat(ctx, chatMsgs, toolsList, masterKey)
		if err != nil {
			slog.Error("engine: LLM call failed", "session_id", sessionID, "turn", turn, "error", err)
			return "", "", nil, events, fmt.Errorf("llm call: %w", err)
		}

		if len(resp.Choices) == 0 {
			slog.Error("engine: no choices in LLM response", "session_id", sessionID)
			return "", "", nil, events, fmt.Errorf("no choices in response")
		}

		modelCalls++
		totalTokensInput += resp.Usage.PromptTokens
		totalTokensOutput += resp.Usage.CompletionTokens

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
			agentMsg := &models.AgentMessage{
				ID:               msgID,
				SessionID:        sessionID,
				Role:             "assistant",
				Content:          text,
				ToolCalls:        allToolCalls,
				ReasoningContent: reasoningContent,
				TokensInput:      totalTokensInput,
				TokensOutput:     totalTokensOutput,
				ModelCalls:       modelCalls,
				CreatedAt:        time.Now(),
			}
			e.session.AppendMessage(ctx, agentMsg)
			return text, reasoningContent, allToolCalls, events, nil
		} else {
			slog.Debug("engine: tool calls in response", "session_id", sessionID, "turn", turn, "num_tool_calls", len(toolCalls), "text_len", len(text))
		}

		chatMsgs = append(chatMsgs, ChatMessage{
			Role:             "assistant",
			Content:          text,
			ToolCalls:        toolCalls,
			ReasoningContent: reasoningContent,
		})

		if onReasoning != nil && reasoningContent != "" {
			chunk(reasoningContent, 8, func(s string) {
				onReasoning(s)
				time.Sleep(12 * time.Millisecond)
			})
		}

		for _, tc := range toolCalls {
			if onToolCall != nil {
				onToolCall(tc.Function.Name, tc.ID, reasoningContent)
			}
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			allToolCalls = append(allToolCalls, models.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			})

			toolDef, ok := toolLookup[tc.Function.Name]
			if !ok {
				toolDef, ok = e.registry.Get(tc.Function.Name)
			}
			if !ok {
				chatMsgs = append(chatMsgs, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name)})
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

			result, err := toolDef.Handler([]byte(tc.Function.Arguments), toolCtx)
			if err != nil {
				chatMsgs = append(chatMsgs, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: fmt.Sprintf("error: %s", err.Error())})
				if onToolResult != nil {
					onToolResult(tc.Function.Name, tc.Function.Arguments, "", err.Error())
				}
			} else {
				resultJSON, _ := json.Marshal(result)
				chatMsgs = append(chatMsgs, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: string(resultJSON)})
				if onToolResult != nil {
					onToolResult(tc.Function.Name, tc.Function.Arguments, string(resultJSON), "")
				}
			}
		}
	}

	return "", "", allToolCalls, events, fmt.Errorf("max turns reached")
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

func (e *Engine) GetRegistry() *ToolRegistry {
	return e.registry
}

func (e *Engine) SessionStore() *SessionStore {
	return e.session
}

func (e *Engine) SetLLMClient(llm *LLMClient) {
	e.llm = llm
}

func (e *Engine) SetToolAllowedDomains(domains []string) {
	e.toolAllowedDomains = domains
}

func (e *Engine) HandleSlashCommand(ctx context.Context, sessionID string, command string, orgID string, masterKey []byte) (any, error) {
	cmd := strings.TrimSpace(command)
	switch cmd {
	case "skills":
		return e.listSkills(ctx, orgID)
	case "agents":
		return e.listAgents(ctx, orgID)
	case "new":
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
		llmClient = NewLLMClient(mc.BaseURL, mc.Model, mc.APIKeyEncrypted)
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
	newSession, err := e.session.CreateSession(ctx, oldSession.AgentID, oldSession.NotebookID, oldSession.UserID, oldSession.MaxTurns, oldSession.MaxTokens, nil)
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
				llmClient = NewLLMClient(mc.BaseURL, mc.Model, mc.APIKeyEncrypted)
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
func (e *Engine) buildNotebookContext(notebookID string) string {
	if notebookID == "" {
		return "No notebook selected."
	}

	var title string
	var connectorID *string
	err := e.pool.QueryRow(context.Background(),
		`SELECT title, connector_id FROM notebooks WHERE id = $1`, notebookID).
		Scan(&title, &connectorID)
	if err != nil {
		return fmt.Sprintf("Current notebook: %s", notebookID)
	}

	ctx := fmt.Sprintf("Current notebook: %s (title: %q)", notebookID, title)

	if connectorID != nil && *connectorID != "" {
		var connName, connType string
		err := e.pool.QueryRow(context.Background(),
			`SELECT name, type FROM connectors WHERE id = $1`, *connectorID).
			Scan(&connName, &connType)
		if err == nil {
			ctx += fmt.Sprintf("\nConnector: %q (type: %s, id: %s)", connName, connType, *connectorID)
			ctx += "\nNotebook cells: type 'code' with language 'sql' for database queries, type 'text' with language 'markdown' for documentation."
			ctx += "\nCharts: Use create_chart to turn a cell's table output into a chart. Types: bar, stacked_bar, line, area, scatter, pie, donut, timeline, hierarchy_tree. For timeline: use time_column, end_time_column (optional), label_column. For hierarchy_tree: use id_column, parent_id_column, label_column. Use update_chart to modify an existing chart's config. The frontend renders automatically from saved config."
		}
	}

	// Add cell count information
	var cellCount int
	var codeCellCount int
	e.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM cells WHERE notebook_id = $1`, notebookID).Scan(&cellCount)
	e.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM cells WHERE notebook_id = $1 AND type = 'code'`, notebookID).Scan(&codeCellCount)
	if cellCount > 0 {
		ctx += fmt.Sprintf("\nCells: %d total (code: %d)", cellCount, codeCellCount)
		ctx += "\nUse get_notebook_context tool to read full cell contents."
	}

	return ctx
}
