package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type MCPClient struct {
	Name    string
	Type    string
	Command string
	Args    []string
	Env     map[string]string
	Stdio   *io.ReadCloser
	HTTPURL string
}

func RegisterMCPTools(reg *ToolRegistry, servers []*MCPClient) {
	for _, srv := range servers {
		if srv.Type == "http" && srv.HTTPURL != "" {
			reg.Register(&ToolDef{
				Function: struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Parameters  any    `json:"parameters"`
				}{
					Name:        srv.Name + "_list_tools",
					Description: fmt.Sprintf("List available tools from MCP server %s", srv.Name),
					Parameters:  "{}",
				},
				Handler: makeMCPToolListHandler(srv),
			})
			reg.Register(&ToolDef{
				Function: struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Parameters  any    `json:"parameters"`
				}{
					Name:        srv.Name + "_call_tool",
					Description: fmt.Sprintf("Call a tool on MCP server %s", srv.Name),
					Parameters:  `{"type":"object","properties":{"tool":{"type":"string"},"arguments":{"type":"object"}},"required":["tool"]}`,
				},
				Handler: makeMCPToolCallHandler(srv),
			})
		}
	}
}

func makeMCPToolListHandler(srv *MCPClient) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		httpCtx, cancel := context.WithTimeout(ctx.Context, 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(httpCtx, "GET", srv.HTTPURL+"/tools/list", nil)
		if err != nil {
			return nil, fmt.Errorf("mcp list tools: %w", err)
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("mcp list tools: %w", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode mcp response: %w", err)
		}
		return result, nil
	}
}

func makeMCPToolListHandlerHTTP(url string) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		httpCtx, cancel := context.WithTimeout(ctx.Context, 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(httpCtx, "GET", url+"/tools/list", nil)
		if err != nil {
			return nil, fmt.Errorf("mcp list tools: %w", err)
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("mcp list tools: %w", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode mcp response: %w", err)
		}
		return result, nil
	}
}

func makeMCPToolCallHandlerHTTP(url string) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		payload := map[string]any{
			"tool":      req.Tool,
			"arguments": req.Arguments,
		}
		body, _ := json.Marshal(payload)

		httpCtx, cancel := context.WithTimeout(ctx.Context, 60*time.Second)
		defer cancel()

		req2, err := http.NewRequestWithContext(httpCtx, "POST", url+"/tools/call", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req2.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req2)
		if err != nil {
			return nil, fmt.Errorf("mcp call tool: %w", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode mcp response: %w", err)
		}
		return result, nil
	}
}

func makeMCPToolCallHandler(srv *MCPClient) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		payload := map[string]any{
			"tool":      req.Tool,
			"arguments": req.Arguments,
		}
		body, _ := json.Marshal(payload)

		httpCtx, cancel := context.WithTimeout(ctx.Context, 60*time.Second)
		defer cancel()

		req2, err := http.NewRequestWithContext(httpCtx, "POST", srv.HTTPURL+"/tools/call", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req2.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req2)
		if err != nil {
			return nil, fmt.Errorf("mcp call tool: %w", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode mcp response: %w", err)
		}
		return result, nil
	}
}
