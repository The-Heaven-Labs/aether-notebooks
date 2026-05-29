package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/heavenlabs/hnb/internal/models"
)

type Engine struct {
	registry *ToolRegistry
	session  *SessionStore
	llm      *LLMClient
	pool     *pgxpool.Pool
	mu       sync.Mutex
}

func NewEngine(pool *pgxpool.Pool) *Engine {
	reg := NewToolRegistry()
	RegisterNotebookTools(reg, pool)
	RegisterChartTools(reg, pool)
	RegisterAgentTools(reg, pool)
	RegisterPlatformTools(reg, pool)

	return &Engine{
		registry: reg,
		session:  NewSessionStore(pool),
		pool:     pool,
	}
}

func (e *Engine) ProcessMessage(ctx context.Context, sessionID string, userMessage string, tools []*ToolDef, masterKey []byte, onToken func(string), onReasoning func(string), onToolCall func(string, string, string), onToolResult func(string, string, string, string)) (string, string, []models.ToolCall, []EngineEvent, error) {
	var events []EngineEvent
	session, err := e.session.GetSession(ctx, sessionID)
	if err != nil {
		return "", "", nil, events, fmt.Errorf("get session: %w", err)
	}

	var agent models.Agent
	var systemPrompt string
	var skillIDs []byte
	var mcpServers []byte
	err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, description, model_config_id, subagent_model_config_id, system_prompt, array_to_json(skill_ids)::text, mcp_servers, folder_id, created_by, created_at, updated_at FROM agents WHERE id = $1`, session.AgentID).Scan(
		&agent.ID, &agent.OrgID, &agent.Name, &agent.Description, &agent.ModelConfigID, &agent.SubagentModelConfigID, &systemPrompt, &skillIDs, &mcpServers, &agent.FolderID, &agent.CreatedBy, &agent.CreatedAt, &agent.UpdatedAt)
	if err != nil {
		return "", "", nil, events, fmt.Errorf("get agent: %w", err)
	}
	if skillIDs != nil {
		json.Unmarshal(skillIDs, &agent.SkillIDs)
	}
	if mcpServers != nil {
		json.Unmarshal(mcpServers, &agent.MCPServers)
	}

	llmClient := e.llm
	if agent.ModelConfigID != nil && *agent.ModelConfigID != "" {
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
	chatMsgs = append(chatMsgs, ChatMessage{Role: "user", Content: userMessage})

	toolsList := make([]OpenAITool, len(tools))
	for i, t := range tools {
		oat, err := t.ToOpenAITool()
		if err != nil {
			return "", "", nil, events, fmt.Errorf("convert tool: %w", err)
		}
		toolsList[i] = oat
	}

	const maxTurns = 15
	var allToolCalls []models.ToolCall
	totalTokensInput := 0
	totalTokensOutput := 0
	modelCalls := 0

	for turn := 0; turn < maxTurns; turn++ {
		resp, err := llmClient.Chat(ctx, chatMsgs, toolsList, masterKey)
		if err != nil {
			return "", "", nil, events, fmt.Errorf("llm call: %w", err)
		}

		if len(resp.Choices) == 0 {
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

			toolDef, ok := e.registry.Get(tc.Function.Name)
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

func (e *Engine) HandleSlashCommand(ctx context.Context, sessionID string, command string, masterKey []byte) (any, error) {
	switch command {
	case "skills":
		return e.listSkills(ctx)
	case "agents":
		return e.listAgents(ctx)
	case "new":
		return map[string]string{"session_id": sessionID}, nil
	case "summarize":
		return e.summarizeSession(ctx, sessionID, masterKey)
	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

func (e *Engine) listSkills(ctx context.Context) (map[string]any, error) {
	rows, err := e.pool.Query(ctx, `SELECT id, name, description FROM skills LIMIT 50`)
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

func (e *Engine) listAgents(ctx context.Context) (map[string]any, error) {
	session, err := e.session.GetSession(ctx, "")
	if err != nil {
		return nil, err
	}

	rows, err := e.pool.Query(ctx, `SELECT id, name, description FROM agents WHERE org_id = (SELECT org_id FROM agent_sessions WHERE id = $1) LIMIT 50`, session.AgentID)
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

	if e.llm == nil {
		return map[string]any{"summary": "No LLM client configured"}, nil
	}

	resp, err := e.llm.Chat(ctx, []ChatMessage{{Role: "user", Content: summarizePrompt}}, nil, masterKey)
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) > 0 {
		return map[string]any{"summary": resp.Choices[0].Message.Content}, nil
	}
	return map[string]any{"summary": "Could not generate summary"}, nil
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
