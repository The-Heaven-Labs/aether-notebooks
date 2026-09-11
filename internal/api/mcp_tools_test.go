package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mcpToolsCall(t *testing.T, srv http.Handler, token, tool string, args map[string]any) (int, map[string]any) {
	t.Helper()
	argsJSON, _ := json.Marshal(args)
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": json.RawMessage(argsJSON)},
	})
	req := httptest.NewRequest("POST", "/api/v1/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	return rec.Code, resp
}

func mcpResultText(t *testing.T, resp map[string]any) string {
	t.Helper()
	result, _ := resp["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("empty MCP result content: %v", resp)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	return text
}

// The MCP path builds its own ToolContext — it must populate the running-state
// hooks so agent-driven runs behave identically off the MCP channel.
func TestMCPToolsCallRunCell(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-run-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "MCP Run Org")
	nbID := createNotebook(t, srv, token, "MCP Run NB")
	connID := createConnector(t, srv, token)
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1 AS x", connID)

	code, resp := mcpToolsCall(t, srv, token, "run_cell", map[string]any{"cell_id": cellID})
	if code != 200 {
		t.Fatalf("tools/call: expected 200, got %d: %v", code, resp)
	}
	text := mcpResultText(t, resp)
	if !strings.Contains(text, `"status":"completed"`) {
		t.Fatalf("expected completed run_cell result, got %s", text)
	}
	if !strings.Contains(text, `"column_names":["x"]`) {
		t.Fatalf("expected inline preview in MCP result, got %s", text)
	}
}

// Cancelling an MCP-driven run via the cell Cancel endpoint aborts the query,
// proving the MCP ToolContext registers its cancel func on the Hub.
func TestMCPToolsCallRunCellCancel(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-cancel-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "MCP Cancel Org")
	nbID := createNotebook(t, srv, token, "MCP Cancel NB")
	connID := createConnector(t, srv, token)
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT pg_sleep(8)", connID)

	type outcome struct {
		code int
		resp map[string]any
	}
	done := make(chan outcome, 1)
	go func() {
		code, resp := mcpToolsCall(t, srv, token, "run_cell", map[string]any{"cell_id": cellID})
		done <- outcome{code, resp}
	}()

	// Wait until the Hub knows about the running query, then cancel it.
	cancelled := false
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells/"+cellID+"/cancel", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == 200 {
			cancelled = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !cancelled {
		t.Fatal("cancel endpoint never saw the MCP-driven run (cancel func not registered)")
	}

	select {
	case out := <-done:
		text := mcpResultText(t, out.resp)
		if !strings.Contains(text, "Query cancelled") {
			t.Fatalf("expected cancelled result, got %s", text)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("MCP run did not finish after cancel")
	}
}
