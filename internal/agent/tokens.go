package agent

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

type TokenCounter struct {
	mu       sync.RWMutex
	encodings map[string]*tiktoken.Tiktoken
}

func NewTokenCounter() *TokenCounter {
	return &TokenCounter{encodings: make(map[string]*tiktoken.Tiktoken)}
}

func modelToEncoding(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gpt-4o"), strings.HasPrefix(m, "gpt-4."), strings.HasPrefix(m, "o1"), strings.HasPrefix(m, "o3"):
		return "o200k_base"
	case strings.HasPrefix(m, "gpt-4"), strings.HasPrefix(m, "gpt-3.5"), strings.HasPrefix(m, "gpt-35"):
		return "cl100k_base"
	case strings.HasPrefix(m, "text-davinci"), strings.HasPrefix(m, "code-davinci"):
		return "p50k_base"
	case strings.HasPrefix(m, "deepseek"):
		return "cl100k_base"
	default:
		return "cl100k_base"
	}
}

func (tc *TokenCounter) getEncoding(encoding string) (*tiktoken.Tiktoken, error) {
	tc.mu.RLock()
	tke, ok := tc.encodings[encoding]
	tc.mu.RUnlock()
	if ok {
		return tke, nil
	}

	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tke, ok := tc.encodings[encoding]; ok {
		return tke, nil
	}

	tke, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		return nil, err
	}
	tc.encodings[encoding] = tke
	return tke, nil
}

func (tc *TokenCounter) Count(text, model string) int {
	encoding := modelToEncoding(model)
	tke, err := tc.getEncoding(encoding)
	if err != nil {
		return len(text) / 4
	}
	return len(tke.Encode(text, nil, nil))
}

func (tc *TokenCounter) CountMessages(msgs []ChatMessage, model string) int {
	if len(msgs) == 0 {
		return 0
	}
	total := 0
	encoding := modelToEncoding(model)
	tke, err := tc.getEncoding(encoding)
	if err != nil {
		for _, m := range msgs {
			total += len(m.Content) / 4
		}
		return total
	}

	for _, m := range msgs {
		total += 4
		total += len(tke.Encode(m.Role, nil, nil))
		total += len(tke.Encode(m.Content, nil, nil))
		if m.ReasoningContent != "" {
			total += len(tke.Encode(m.ReasoningContent, nil, nil))
		}
		for _, tc := range m.ToolCalls {
			total += len(tke.Encode(tc.Function.Name, nil, nil))
			total += len(tke.Encode(tc.Function.Arguments, nil, nil))
			total += 2
		}
	}
	total += 3
	return total
}

func (tc *TokenCounter) CountToolDefs(tools []OpenAITool, model string) int {
	if len(tools) == 0 {
		return 0
	}
	encoding := modelToEncoding(model)
	tke, err := tc.getEncoding(encoding)
	if err != nil {
		data, _ := json.Marshal(tools)
		return len(data) / 4
	}

	total := 0
	for _, t := range tools {
		total += len(tke.Encode(t.Function.Name, nil, nil))
		total += len(tke.Encode(t.Function.Description, nil, nil))
		if params, err := json.Marshal(t.Function.Parameters); err == nil {
			total += len(tke.Encode(string(params), nil, nil))
		}
		total += 3
	}
	return total
}

func (tc *TokenCounter) CountText(text, model string) int {
	return tc.Count(text, model)
}
