package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/heavenlabs/hnb/internal/models"
)

func makeWebhookToolDef(t *models.Tool) (*ToolDef, error) {
	url, _ := t.Config["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("webhook tool missing url")
	}
	method, _ := t.Config["method"].(string)
	if method == "" {
		method = "POST"
	}
	headers := make(map[string]string)
	if h, ok := t.Config["headers"].(map[string]any); ok {
		for k, v := range h {
			headers[k] = fmt.Sprintf("%v", v)
		}
	}
	return &ToolDef{
		Type: "function",
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Schema,
		},
		Handler: func(args json.RawMessage, ctx *ToolContext) (any, error) {
			var req *http.Request
			var err error
			if method == "GET" {
				req, err = http.NewRequest(method, url, nil)
			} else {
				req, err = http.NewRequest(method, url, bytes.NewReader(args))
			}
			if err != nil {
				return nil, fmt.Errorf("create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			c, cancel := context.WithTimeout(ctx.Context, 30*time.Second)
			defer cancel()
			resp, err := http.DefaultClient.Do(req.WithContext(c))
			if err != nil {
				return nil, fmt.Errorf("webhook call: %w", err)
			}
			defer resp.Body.Close()
			respBody, _ := io.ReadAll(resp.Body)
			var result any
			json.Unmarshal(respBody, &result)
			return map[string]any{
				"status": resp.StatusCode,
				"body":   result,
			}, nil
		},
	}, nil
}
