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

	"github.com/heavenlabs/hnb/internal/crypto"
)

type LLMClient struct {
	baseURL    string
	model      string
	apiKey     []byte
	httpClient *http.Client
}

type ChatMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []OpenAITool  `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
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
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewLLMClient(baseURL, model string, apiKey []byte) *LLMClient {
	return &LLMClient{
		baseURL: baseURL,
		model:   model,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *LLMClient) Chat(ctx context.Context, messages []ChatMessage, tools []OpenAITool, masterKey []byte) (*ChatResponse, error) {
	reqBody := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
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
	reqBody := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
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
