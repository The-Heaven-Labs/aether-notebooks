package agent

import (
	"context"
	"fmt"
)

type ContextManager struct {
	contextWindow int
}

func NewContextManager(contextWindow int) *ContextManager {
	return &ContextManager{contextWindow: contextWindow}
}

func (cm *ContextManager) CountTokens(text string) int {
	return len(text) / 4
}

func (cm *ContextManager) NeedsSummarization(systemPrompt string, skillPrompts []string, messages []ChatMessage) bool {
	totalTokens := cm.CountTokens(systemPrompt)
	for _, sp := range skillPrompts {
		totalTokens += cm.CountTokens(sp)
	}
	for _, m := range messages {
		totalTokens += cm.CountTokens(m.Content)
	}

	threshold := float64(cm.contextWindow) * 0.8
	return float64(totalTokens) >= threshold
}

func (cm *ContextManager) BuildContext(systemPrompt string, skillPrompts []string, messages []ChatMessage) ([]ChatMessage, error) {
	allMessages := make([]ChatMessage, 0, len(messages)+2)
	allMessages = append(allMessages, ChatMessage{Role: "system", Content: systemPrompt})
	for _, sp := range skillPrompts {
		allMessages = append(allMessages, ChatMessage{Role: "system", Content: sp})
	}
	allMessages = append(allMessages, messages...)
	return allMessages, nil
}

func (cm *ContextManager) SummarizeMessages(ctx context.Context, llm *LLMClient, messages []ChatMessage, masterKey []byte) (string, error) {
	recentMessages := messages
	if len(messages) > 20 {
		recentMessages = messages[len(messages)-20:]
	}

	summarizePrompt := "Summarize the following conversation concisely, preserving key information, decisions, and context:\n\n"
	for _, m := range recentMessages {
		summarizePrompt += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
	}

	resp, err := llm.Chat(ctx, []ChatMessage{{Role: "user", Content: summarizePrompt}}, nil, masterKey)
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}

	if len(resp.Choices) > 0 {
		return resp.Choices[0].Message.Content, nil
	}
	return "", fmt.Errorf("no summary returned")
}

func (cm *ContextManager) GetContextWindow() int {
	return cm.contextWindow
}
