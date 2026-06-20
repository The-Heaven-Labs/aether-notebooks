package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/heavenlabs/hnb/internal/models"
)

func makeWebhookToolDef(t *models.Tool, allowedDomains []string) (*ToolDef, error) {
	rawURL, _ := t.Config["url"].(string)
	if rawURL == "" {
		return nil, fmt.Errorf("webhook tool missing url")
	}
	method, _ := t.Config["method"].(string)
	if method == "" {
		method = "POST"
	}
	contentType, _ := t.Config["content_type"].(string)
	if contentType == "" {
		contentType = "application/json"
	}
	headers := make(map[string]string)
	if h, ok := t.Config["headers"].(map[string]any); ok {
		for k, v := range h {
			headers[k] = fmt.Sprintf("%v", v)
		}
	}

	// Validate scheme at definition time
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
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
			var params map[string]any
			if len(args) > 0 {
				json.Unmarshal(args, &params)
			}
			if err := validateRequiredParams(t.Schema, params); err != nil {
				return nil, err
			}

			// Substitute {{param}} in URL with proper encoding
			resolvedURL := rawURL
			var bodyArgs map[string]any
			if params != nil {
				bodyArgs = make(map[string]any)
				for k, v := range params {
					placeholder := fmt.Sprintf("{{%s}}", k)
					if strings.Contains(resolvedURL, placeholder) {
						val := fmt.Sprintf("%v", v)
						// Encode based on where the placeholder appears
						if idx := strings.Index(resolvedURL, placeholder); idx >= 0 {
							after := resolvedURL[idx+len(placeholder):]
							beforeQ := strings.Index(resolvedURL[:idx], "?")
							afterQ := strings.Index(after, "?")
							if beforeQ >= 0 && afterQ < 0 {
								// Placeholder is in query portion (after ?)
								val = url.QueryEscape(val)
							} else if beforeQ < 0 && afterQ < 0 {
								// No ? in URL at all, placeholder is in path
								val = url.PathEscape(val)
							} else {
								// Ambiguous — encode path-safe
								val = url.PathEscape(val)
							}
						}
						resolvedURL = strings.ReplaceAll(resolvedURL, placeholder, val)
					} else {
						bodyArgs[k] = v
					}
				}
			}

			// Validate resolved URL for SSRF
			if err := validateWebhookURL(resolvedURL, allowedDomains); err != nil {
				return nil, err
			}

			var req *http.Request
			var err error
			switch method {
			case "GET":
				req, err = http.NewRequest(method, resolvedURL, nil)
				if err == nil && len(bodyArgs) > 0 {
					q := req.URL.Query()
					for k, v := range bodyArgs {
						q.Set(k, fmt.Sprintf("%v", v))
					}
					req.URL.RawQuery = q.Encode()
				}
			default:
				var bodyBytes []byte
				if len(bodyArgs) > 0 {
					bodyBytes, _ = json.Marshal(bodyArgs)
				} else if len(args) > 0 {
					bodyBytes = args
				}
				req, err = http.NewRequest(method, resolvedURL, bytes.NewReader(bodyBytes))
			}
			if err != nil {
				return nil, fmt.Errorf("create request: %w", err)
			}
			if req.Body != nil && req.Body != http.NoBody {
				req.Header.Set("Content-Type", contentType)
			}
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
