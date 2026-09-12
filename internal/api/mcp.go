package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

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

const mcpLatestProtocolVersion = "2026-07-28"

var mcpSupportedProtocolVersions = []string{"2025-06-18", "2025-11-25", "2026-07-28"}

func mcpSupportsProtocolVersion(v string) bool {
	for _, supported := range mcpSupportedProtocolVersions {
		if supported == v {
			return true
		}
	}
	return false
}

// handleMCP serves the MCP (Model Context Protocol) endpoint over HTTP.
// Authenticated via Bearer token (personal access token or JWT).
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("mcp handler panic", "panic", rec)
			writeJSON(w, http.StatusInternalServerError, mcpJSONRPCResponse{
				JSONRPC: "2.0", ID: nil,
				Error: &mcpError{Code: -32603, Message: "Internal error"},
			})
		}
	}()

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

	if req.Method == "initialize" {
		s.handleMCPInitialize(w, req)
		return
	}

	// MCP clients send the negotiated version on every request after
	// initialize. Reject unknown versions; a missing header is accepted for
	// older clients.
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !mcpSupportsProtocolVersion(v) {
		writeJSON(w, http.StatusBadRequest, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32600, Message: "Unsupported MCP-Protocol-Version: " + v},
		})
		return
	}

	// JSON-RPC notifications carry no id and expect an empty 202 response.
	if strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "ping":
		writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]interface{}{},
		})
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

	protocolVersion := mcpLatestProtocolVersion
	if mcpSupportsProtocolVersion(params.ProtocolVersion) {
		protocolVersion = params.ProtocolVersion
	}

	serverVersion := s.version
	if serverVersion == "" {
		serverVersion = "dev"
	}

	writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: map[string]interface{}{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    "aether",
				"version": serverVersion,
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
		SetRunningFunc:   s.hub.SetRunning,
		UnsetRunningFunc: s.hub.UnsetRunning,
		SetCancelFunc:    s.hub.SetCancelFunc,
		DeleteCancelFunc: s.hub.DeleteCancelFunc,
	}

	result, err := def.Execute(params.Arguments, ctx)
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

// handleMCPNoStream answers non-POST methods on the MCP endpoint. Aether does
// not offer the optional SSE stream or sessions from the Streamable HTTP
// transport, so GET/DELETE return 405 as permitted by the MCP spec.
func handleMCPNoStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "POST")
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
		"error": "method not allowed: Aether's MCP endpoint is POST-only (no SSE stream or sessions)",
	})
}
