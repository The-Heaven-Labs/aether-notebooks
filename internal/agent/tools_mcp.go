package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/the-heaven-labs/aether/internal/headers"
)

// mcpProtocolVersion is the protocol version proposed in the initialize
// handshake. 2025-06-18 is the Streamable HTTP transport version supported by
// every mainstream MCP server (including AbuseIPDB, SQLite, filesystem servers).
const mcpProtocolVersion = "2025-06-18"

// mcpAccept advertises both JSON and SSE responses per the Streamable HTTP spec.
const mcpAccept = "application/json, text/event-stream"

const mcpClientName = "aether-notebooks"

// mcpHTTPClient speaks the MCP Streamable HTTP transport to a single server.
// It performs the initialize handshake lazily (once per client lifetime, i.e.
// once per agent turn) and reuses the negotiated session id for list/call.
//
// It replaces the previous REST client that issued GET {url}/tools/list and
// POST {url}/tools/call with a non-JSON-RPC body: no MCP server implements
// those paths, so every call decoded a 4xx HTML page as JSON and failed with
// "invalid character '<'".
type mcpHTTPClient struct {
	baseURL    string
	hdrs       map[string]string
	httpClient *http.Client

	mu              sync.Mutex
	initialized     bool
	sessionID       string // from the Mcp-Session-Id response header
	protocolVersion string // negotiated in initialize

	initOnce sync.Once
	initErr  error

	nextID atomic.Int64
}

// newMCPHTTPClient builds a client whose http.Client re-applies the configured
// headers + session id on every redirect. Go's default client strips
// Authorization on cross-host redirects; API-gateway-fronted MCP servers (vanity
// URL -> real endpoint) would otherwise lose auth, so we re-apply it here.
func newMCPHTTPClient(baseURL string, hdrs map[string]string) *mcpHTTPClient {
	c := &mcpHTTPClient{baseURL: baseURL, hdrs: hdrs}
	c.httpClient = &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			baseReq := via[len(via)-1]
			for _, h := range []string{"Accept", "Mcp-Session-Id", "Mcp-Protocol-Version"} {
				if v := baseReq.Header.Get(h); v != "" {
					req.Header.Set(h, v)
				}
			}
			headers.Apply(req, c.hdrs)
			return nil
		},
	}
	return c
}

func setMCPHeaders(req *http.Request, hdrs map[string]string) {
	req.Header.Set("Accept", mcpAccept)
	req.Header.Set("Content-Type", "application/json")
	headers.Apply(req, hdrs) // skips reserved names (incl. content-type)
}

// ensureInitialized performs the initialize handshake once. It is safe under
// concurrent calls from list+call handlers: sync.Once guarantees a single
// initialize regardless of how many goroutines race in.
func (c *mcpHTTPClient) ensureInitialized(ctx context.Context) error {
	c.initOnce.Do(func() {
		c.initErr = c.initialize(ctx)
	})
	return c.initErr
}

func (c *mcpHTTPClient) initialize(ctx context.Context) error {
	result, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": mcpClientName, "version": "0.81.0"},
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initialized = true
	if pv, ok := result["protocolVersion"].(string); ok && pv != "" {
		c.protocolVersion = pv
	} else {
		c.protocolVersion = mcpProtocolVersion
	}
	return nil
}

func (c *mcpHTTPClient) call(ctx context.Context, method string, params any) (map[string]any, error) {
	return c.request(ctx, method, params)
}

func (c *mcpHTTPClient) request(ctx context.Context, method string, params any) (map[string]any, error) {
	id := c.nextID.Add(1)
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp %s: %w", method, err)
	}
	setMCPHeaders(req, c.hdrs)
	if c.protocolVersion != "" {
		req.Header.Set("Mcp-Protocol-Version", c.protocolVersion)
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp %s: %w", method, err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 400 || (!strings.Contains(ct, "application/json") && !strings.Contains(ct, "text/event-stream")) {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("mcp %s: server returned HTTP %d (%s): %q", method, resp.StatusCode, ct, snippet)
	}

	var result map[string]any
	if strings.Contains(ct, "text/event-stream") {
		res, err := readSSEMatch(resp.Body, id)
		if err != nil {
			return nil, fmt.Errorf("mcp %s: %w", method, err)
		}
		result = res
	} else {
		var resp2 struct {
			Result map[string]any `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&resp2); err != nil {
			return nil, fmt.Errorf("mcp %s: decode response: %w", method, err)
		}
		if resp2.Error != nil {
			return nil, fmt.Errorf("mcp %s: error %d: %s", method, resp2.Error.Code, resp2.Error.Message)
		}
		result = resp2.Result
	}

	// Servers MAY rotate the session id on any response; always capture the
	// latest so subsequent requests carry it.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}
	return result, nil
}

// readSSEMatch scans an SSE stream for the JSON-RPC message whose id matches
// requestID and returns its result. Notifications (no id) are ignored.
func readSSEMatch(r io.Reader, requestID int64) (map[string]any, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	wantStr := fmt.Sprintf("%d", requestID)
	for scanner.Scan() {
		line := scanner.Text()
		text, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		raw := strings.TrimSpace(text)
		if raw == "" {
			continue
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Result map[string]any  `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if len(msg.ID) == 0 {
			continue // notification
		}
		var mid int64
		var midStr string
		if json.Unmarshal(msg.ID, &mid) == nil && mid == requestID {
			// numeric id match
		} else if json.Unmarshal(msg.ID, &midStr) == nil && midStr == wantStr {
			// string id match
		} else {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("error %d: %s", msg.Error.Code, msg.Error.Message)
		}
		return msg.Result, nil
	}
	return nil, fmt.Errorf("no response with matching id in event stream")
}

// handleListTools / handleCallTool adapt the client to the engine's ToolHandler.
func (c *mcpHTTPClient) handleListTools() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		if err := c.ensureInitialized(ctx.Context); err != nil {
			return nil, err
		}
		return c.call(ctx.Context, "tools/list", nil)
	}
}

func (c *mcpHTTPClient) handleCallTool() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := c.ensureInitialized(ctx.Context); err != nil {
			return nil, err
		}
		return c.call(ctx.Context, "tools/call", map[string]any{
			"name":      req.Tool,
			"arguments": req.Arguments,
		})
	}
}
