package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/the-heaven-labs/aether/internal/crypto"
)

type LLMClient struct {
	baseURL       string
	model         string
	apiKey        []byte
	defaultParams map[string]any
	httpClient    *http.Client
}

type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"`
}

type ChatMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content,omitempty"`
	ToolCalls        []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	MultiContent     []ContentPart  `json:"-"`
}

func (m ChatMessage) MarshalJSON() ([]byte, error) {
	if len(m.MultiContent) > 0 {
		msg := map[string]any{
			"role": m.Role,
			"content": m.MultiContent,
		}
		if len(m.ToolCalls) > 0 {
			msg["tool_calls"] = m.ToolCalls
		}
		if m.ReasoningContent != "" {
			msg["reasoning_content"] = m.ReasoningContent
		}
		return json.Marshal(msg)
	}
	type alias ChatMessage
	return json.Marshal(alias(m))
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []OpenAITool  `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
	Extra    map[string]any `json:"-"`
}

func (r ChatRequest) MarshalJSON() ([]byte, error) {
	type alias ChatRequest
	base, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	if len(r.Extra) == 0 {
		return base, nil
	}
	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	for k, v := range r.Extra {
		m[k] = v
	}
	return json.Marshal(m)
}

type OpenAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
	ToolCalls    []ToolCall  `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Usage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type TokenBreakdown struct {
	Input               int `json:"input"`
	Output              int `json:"output"`
	Reasoning           int `json:"reasoning"`
	CacheRead           int `json:"cache_read"`
	ModelCalls          int `json:"model_calls"`
	SystemPrompt        int `json:"system_prompt"`
	SkillOverride       int `json:"skill_override"`
	History             int `json:"history"`
	UserMessage         int `json:"user_message"`
	ToolDefinitions     int `json:"tool_definitions"`
	ToolCalls           int `json:"tool_calls"`
	ToolResults         int `json:"tool_results"`
	DurationMs          int `json:"duration_ms"`
}

func NewLLMClient(baseURL, model string, apiKey []byte, defaultParams map[string]any) *LLMClient {
	return &LLMClient{
		baseURL:      baseURL,
		model:        model,
		apiKey:       apiKey,
		defaultParams: defaultParams,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *LLMClient) Chat(ctx context.Context, messages []ChatMessage, tools []OpenAITool, masterKey []byte) (*ChatResponse, error) {
	extra := make(map[string]any)
	for k, v := range c.defaultParams {
		if k == "compaction_threshold" || k == "reasoning_effort_options" {
			continue
		}
		extra[k] = v
	}
	reqBody := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
		Extra:    extra,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	reqURL := c.baseURL + "/chat/completions"

	decryptedKey, err := crypto.Decrypt(c.apiKey, masterKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt api key: %w", err)
	}

	const maxRetries = 2
	const baseDelay = 500 * time.Millisecond
	const maxDelay = 10 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt-1)))
			if delay > maxDelay {
				delay = maxDelay
			}
			time.Sleep(delay)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+string(decryptedKey))
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http call: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			var chatResp ChatResponse
			if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
				resp.Body.Close()
				return nil, fmt.Errorf("decode response: %w", err)
			}
			resp.Body.Close()
			return &chatResp, nil
		}

		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		retryable := resp.StatusCode == 429 || resp.StatusCode == 503 || resp.StatusCode == 504 || resp.StatusCode == 529 || resp.StatusCode >= 500
		if !retryable || attempt == maxRetries {
			return nil, fmt.Errorf("llm error %d: %s", resp.StatusCode, string(errBody))
		}
	}

	return nil, fmt.Errorf("llm error: max retries exceeded")
}

type StreamResponse struct {
	Type     string `json:"type"`
	Content  string `json:"content,omitempty"`
	ToolCall *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Args string `json:"arguments"`
	} `json:"tool_call,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
	Usage        Usage  `json:"usage,omitempty"`
}

func (c *LLMClient) ChatStream(ctx context.Context, messages []ChatMessage, tools []OpenAITool, masterKey []byte, onToken func(string)) error {
	extra := make(map[string]any)
	for k, v := range c.defaultParams {
		if k == "compaction_threshold" || k == "reasoning_effort_options" {
			continue
		}
		extra[k] = v
	}
	reqBody := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
		Extra:    extra,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	decryptedKey, err := crypto.Decrypt(c.apiKey, masterKey)
	if err != nil {
		return fmt.Errorf("decrypt api key: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+string(decryptedKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("llm error %d: %s", resp.StatusCode, string(errBody))
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		line, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("stream decode: %w", err)
		}

		if delim, ok := line.(json.Delim); ok && delim == '[' {
			continue
		}

		var sse map[string]any
		if err := decoder.Decode(&sse); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		if choices, ok := sse["choices"].([]any); ok && len(choices) > 0 {
			if delta, ok := choices[0].(map[string]any); ok {
				if content, ok := delta["delta"].(map[string]any); ok {
					if tc, ok := content["tool_calls"].([]any); ok && len(tc) > 0 {
						if toolCall, ok := tc[0].(map[string]any); ok {
							if args, ok := toolCall["function"].(map[string]any); ok {
								if name, ok := args["name"].(string); ok {
									onToken("[TOOL_CALL:" + name + "]")
								}
							}
						}
					}
					if c, ok := content["content"].(string); ok {
						onToken(c)
					}
				}
			}
		}
	}

	return nil
}
