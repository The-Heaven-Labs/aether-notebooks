package api

import (
	"encoding/json"
	"net/http"

	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/auth"
)

type mcpJSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpJSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *mcpError   `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string      `json:"name"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// handleMCP serves the MCP (Model Context Protocol) endpoint over HTTP.
// Authenticated via Bearer token (personal access token or JWT).
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	var req mcpJSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: nil,
			Error: &mcpError{Code: -32700, Message: "Parse error"},
		})
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSON(w, http.StatusBadRequest, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32600, Message: "Invalid Request: must use jsonrpc 2.0"},
		})
		return
	}

	switch req.Method {
	case "initialize":
		s.handleMCPInitialize(w, req)
	case "tools/list":
		s.handleMCPToolsList(w, req, claims)
	case "tools/call":
		s.handleMCPToolsCall(w, req, claims, r)
	default:
		writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32601, Message: "Method not found: " + req.Method},
		})
	}
}

func (s *Server) handleMCPInitialize(w http.ResponseWriter, req mcpJSONRPCRequest) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    "aether",
				"version": "1.0.0",
			},
		},
	})
}

func (s *Server) handleMCPToolsList(w http.ResponseWriter, req mcpJSONRPCRequest, claims *auth.Claims) {
	registry := s.agentEngine.GetRegistry()
	defs := registry.List()

	tools := make([]mcpTool, 0, len(defs))
	for _, d := range defs {
		if d.Function.Name == "" {
			continue
		}
		schema := resolveMCPSchema(d.Function.Parameters)
		tools = append(tools, mcpTool{
			Name:        d.Function.Name,
			Description: d.Function.Description,
			InputSchema: schema,
		})
	}

	writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	})
}

func (s *Server) handleMCPToolsCall(w http.ResponseWriter, req mcpJSONRPCRequest, claims *auth.Claims, r *http.Request) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32602, Message: "Invalid tool call params"},
		})
		return
	}

	registry := s.agentEngine.GetRegistry()
	def, ok := registry.Get(params.Name)
	if !ok {
		writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32602, Message: "Unknown tool: " + params.Name},
		})
		return
	}

	ctx := &agent.ToolContext{
		Context:   r.Context(),
		UserID:    claims.UserID,
		OrgID:     claims.OrgID,
		OrgRole:   claims.Role,
		DB:        s.db.Pool,
		MasterKey: s.masterKey,
		BroadcastFunc: func(notebookID string, msg interface{}) {
			s.hub.Broadcast(notebookID, msg)
		},
	}

	result, err := def.Handler(params.Arguments, ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "Error: " + err.Error()},
				},
				"isError": true,
			},
		})
		return
	}

	resultJSON, _ := json.Marshal(result)
	writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": string(resultJSON)},
			},
		},
	})
}

// resolveMCPSchema converts a tool's parameters schema (string or map) to the MCP inputSchema format.
func resolveMCPSchema(params interface{}) map[string]interface{} {
	switch p := params.(type) {
	case string:
		var m map[string]interface{}
		if json.Unmarshal([]byte(p), &m) == nil {
			// If it has a top-level "properties" key, use it as-is
			if _, ok := m["properties"]; ok {
				return m
			}
			// Otherwise wrap in an object schema
			return map[string]interface{}{
				"type":       "object",
				"properties": m,
			}
		}
	case map[string]interface{}:
		return p
	case json.RawMessage:
		var m map[string]interface{}
		if json.Unmarshal(p, &m) == nil {
			if _, ok := m["properties"]; ok {
				return m
			}
			return map[string]interface{}{
				"type":       "object",
				"properties": m,
			}
		}
	}
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
