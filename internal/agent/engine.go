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
	registry    *ToolRegistry
	session     *SessionStore
	llm         *LLMClient
	pool        *pgxpool.Pool
	mu          sync.Mutex
	rateLimiter *RateLimiter
}

func NewEngine(pool *pgxpool.Pool) *Engine {
	reg := NewToolRegistry()
	RegisterNotebookTools(reg, pool)
	RegisterAgentTools(reg, pool)
	RegisterPlatformTools(reg, pool)

	return &Engine{
		registry:    reg,
		session:     NewSessionStore(pool),
		pool:        pool,
		rateLimiter: NewRateLimiter(pool),
	}
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
	err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, description, model_config_id, subagent_model_config_id, system_prompt, array_to_json(skill_ids)::text, folder_id, created_by, created_at, updated_at FROM agents WHERE id = $1`, session.AgentID).Scan(
		&agent.ID, &agent.OrgID, &agent.Name, &agent.Description, &agent.ModelConfigID, &agent.SubagentModelConfigID, &systemPrompt, &skillIDs, &agent.FolderID, &agent.CreatedBy, &agent.CreatedAt, &agent.UpdatedAt)
	if err != nil {
		return "", "", nil, events, fmt.Errorf("get agent: %w", err)
	}
	if skillIDs != nil {
		json.Unmarshal(skillIDs, &agent.SkillIDs)
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

	ok, err := e.rateLimiter.CheckAndUpdateTokens(ctx, sessionID, 0, 0)
	if err != nil {
		slog.Error("rate limit check failed", "session_id", sessionID, "error", err)
		return "", "", nil, events, fmt.Errorf("rate limit check: %w", err)
	}
	if !ok {
		slog.Warn("rate limit exceeded", "session_id", sessionID)
		return "", "", nil, events, fmt.Errorf("rate limit exceeded: session has reached max turns or tokens")
	}

	var skillPrompts []string
	if len(agent.SkillIDs) > 0 {
		rows, err := e.pool.Query(ctx, `SELECT system_prompt FROM skills WHERE id = ANY($1) AND system_prompt IS NOT NULL AND system_prompt != ''`, agent.SkillIDs)
		if err == nil {
			for rows.Next() {
				var prompt string
				if err := rows.Scan(&prompt); err == nil && prompt != "" {
					skillPrompts = append(skillPrompts, prompt)
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

	chatMsgs := make([]ChatMessage, 0)
	notebookCtx := fmt.Sprintf("Current notebook: %s", session.NotebookID)
	if systemPrompt != "" {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: systemPrompt + "\n\n" + notebookCtx})
	} else {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: notebookCtx})
	}
	for _, m := range messages {
		msg := ChatMessage{Role: m.Role, Content: m.Content}
		if m.ReasoningContent != "" {
			msg.ReasoningContent = m.ReasoningContent
		}
		chatMsgs = append(chatMsgs, msg)
	}
	for _, sp := range skillPrompts {
		chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: sp})
	}
	chatMsgs = append(chatMsgs, ChatMessage{Role: "user", Content: userMessage})

	userMsgID := uuid.New().String()
	e.session.AppendMessage(ctx, &models.AgentMessage{
		ID:        userMsgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   userMessage,
		CreatedAt: time.Now(),
	})

	allTools := make([]*ToolDef, len(tools))
	copy(allTools, tools)

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

	const maxTurns = 15
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
			allToolCalls = append(allToolCalls, models.ToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
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
				Context:    ctx,
				UserID:     session.UserID,
				OrgID:      agent.OrgID,
				OrgRole:    orgRole,
				NotebookID: session.NotebookID,
				SessionID:  sessionID,
				DB:         e.pool,
				TurnCount:  turn,
				Events:     &events,
				MasterKey:  masterKey,
				OnEvent:    onEvent,
			}

			result, err := toolDef.Handler(json.RawMessage(tc.Function.Arguments), toolCtx)
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

func (e *Engine) GetRegistry() *ToolRegistry {
	return e.registry
}

func (e *Engine) SessionStore() *SessionStore {
	return e.session
}

func (e *Engine) SetLLMClient(llm *LLMClient) {
	e.llm = llm
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
