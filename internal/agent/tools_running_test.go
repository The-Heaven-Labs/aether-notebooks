package agent_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/the-heaven-labs/aether/internal/agent"
)

type runningHookCapture struct {
	mu           sync.Mutex
	broadcasts   []map[string]any
	setRunning   [][3]string // cellID, notebookID (startedAt asserted non-zero separately)
	unsetRunning []string
	setCancel    []string
	deleteCancel []string
	cancelFuncs  map[string]context.CancelFunc
	startedAts   []time.Time
}

func newRunningHookCapture() *runningHookCapture {
	return &runningHookCapture{cancelFuncs: map[string]context.CancelFunc{}}
}

func (c *runningHookCapture) broadcast(notebookID string, msg any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := msg.(map[string]any); ok {
		c.broadcasts = append(c.broadcasts, m)
	}
}

func (c *runningHookCapture) wire(ctx *agent.ToolContext) {
	ctx.BroadcastFunc = c.broadcast
	ctx.SetRunningFunc = func(cellID, notebookID string, startedAt time.Time) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.setRunning = append(c.setRunning, [3]string{cellID, notebookID})
		c.startedAts = append(c.startedAts, startedAt)
	}
	ctx.UnsetRunningFunc = func(cellID string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.unsetRunning = append(c.unsetRunning, cellID)
	}
	ctx.SetCancelFunc = func(cellID string, cancel context.CancelFunc) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.setCancel = append(c.setCancel, cellID)
		c.cancelFuncs[cellID] = cancel
	}
	ctx.DeleteCancelFunc = func(cellID string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.deleteCancel = append(c.deleteCancel, cellID)
		delete(c.cancelFuncs, cellID)
	}
}

func (c *runningHookCapture) executingBroadcasts() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]any
	for _, b := range c.broadcasts {
		if b["type"] == "cell_executing" {
			out = append(out, b)
		}
	}
	return out
}

// run_cell must signal start-of-run (broadcast + SetRunning + cancel func)
// and always clear it afterwards.
func TestAgentRunCellEmitsRunningLifecycle(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	connID, masterKey := createTestPGConnector(t, db, orgID, userID)
	cellID := createTestCellRow(t, db, nbID, connID, "SELECT 1 AS x")

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	runCellDef, _ := reg.Get("run_cell")
	ctx := setupToolContext(t, db, orgID, userID, nbID)
	ctx.MasterKey = masterKey
	cap := newRunningHookCapture()
	cap.wire(ctx)

	args, _ := json.Marshal(map[string]any{"cell_id": cellID})
	result, err := runCellDef.Handler(args, ctx)
	if err != nil {
		t.Fatalf("run cell: %v", err)
	}
	if result.(map[string]any)["status"] != "completed" {
		t.Fatalf("expected completed, got %v", result)
	}

	exec := cap.executingBroadcasts()
	if len(exec) != 1 {
		t.Fatalf("expected 1 cell_executing broadcast, got %d (%v)", len(exec), cap.broadcasts)
	}
	if exec[0]["cell_id"] != cellID {
		t.Fatalf("wrong cell_id in cell_executing: %v", exec[0])
	}
	if exec[0]["user_email"] != "agent@aether" {
		t.Fatalf("cell_executing must carry user_email for scroll discrimination: %v", exec[0])
	}
	if len(cap.setRunning) != 1 || cap.setRunning[0][0] != cellID || cap.setRunning[0][1] != nbID {
		t.Fatalf("SetRunning not called correctly: %v", cap.setRunning)
	}
	if len(cap.startedAts) != 1 || cap.startedAts[0].IsZero() {
		t.Fatal("SetRunning startedAt must be set")
	}
	if len(cap.unsetRunning) != 1 || cap.unsetRunning[0] != cellID {
		t.Fatalf("UnsetRunning not called: %v", cap.unsetRunning)
	}
	if len(cap.setCancel) != 1 || cap.setCancel[0] != cellID {
		t.Fatalf("cancel func not registered: %v", cap.setCancel)
	}
	if len(cap.deleteCancel) != 1 || cap.deleteCancel[0] != cellID {
		t.Fatalf("cancel func not deleted: %v", cap.deleteCancel)
	}
}

// The error path must also clear running state.
func TestAgentRunCellClearsRunningOnError(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	connID, masterKey := createTestPGConnector(t, db, orgID, userID)
	cellID := createTestCellRow(t, db, nbID, connID, "SELECT * FROM nonexistent_table_xyz")

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	runCellDef, _ := reg.Get("run_cell")
	ctx := setupToolContext(t, db, orgID, userID, nbID)
	ctx.MasterKey = masterKey
	cap := newRunningHookCapture()
	cap.wire(ctx)

	args, _ := json.Marshal(map[string]any{"cell_id": cellID})
	result, err := runCellDef.Handler(args, ctx)
	if err != nil {
		t.Fatalf("query error must be a result, not a Go error: %v", err)
	}
	if result.(map[string]any)["status"] != "error" {
		t.Fatalf("expected error, got %v", result)
	}
	if len(cap.setRunning) != 1 {
		t.Fatalf("SetRunning must fire before the failing query: %v", cap.setRunning)
	}
	if len(cap.unsetRunning) != 1 || cap.unsetRunning[0] != cellID {
		t.Fatalf("UnsetRunning must fire on error: %v", cap.unsetRunning)
	}
	if len(cap.deleteCancel) != 1 {
		t.Fatalf("cancel func must be deleted on error: %v", cap.deleteCancel)
	}
}

// The already-has-results skip path performs no execution and must not touch
// running state.
func TestAgentRunCellSkippedTouchesNoRunningState(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	connID, masterKey := createTestPGConnector(t, db, orgID, userID)
	cellID := createTestCellRow(t, db, nbID, connID, "SELECT 1")

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	runCellDef, _ := reg.Get("run_cell")
	ctx := setupToolContext(t, db, orgID, userID, nbID)
	ctx.MasterKey = masterKey
	cap := newRunningHookCapture()
	cap.wire(ctx)

	args, _ := json.Marshal(map[string]any{"cell_id": cellID})
	if _, err := runCellDef.Handler(args, ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Reset captures; the skipped run must add nothing.
	cap.mu.Lock()
	cap.broadcasts = nil
	cap.setRunning = nil
	cap.unsetRunning = nil
	cap.setCancel = nil
	cap.deleteCancel = nil
	cap.mu.Unlock()

	result, err := runCellDef.Handler(args, ctx)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if result.(map[string]any)["status"] != "skipped" {
		t.Fatalf("expected skipped, got %v", result)
	}
	if len(cap.executingBroadcasts()) != 0 || len(cap.setRunning) != 0 || len(cap.setCancel) != 0 {
		t.Fatal("skipped run must not emit running state")
	}
}

// Invoking the registered cancel func aborts the query like the Cancel button.
func TestAgentRunCellCancelAbortsQuery(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	connID, masterKey := createTestPGConnector(t, db, orgID, userID)
	cellID := createTestCellRow(t, db, nbID, connID, "SELECT pg_sleep(10)")

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	runCellDef, _ := reg.Get("run_cell")
	ctx := setupToolContext(t, db, orgID, userID, nbID)
	ctx.MasterKey = masterKey
	cap := newRunningHookCapture()
	cap.wire(ctx)

	type outcome struct {
		result any
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		args, _ := json.Marshal(map[string]any{"cell_id": cellID})
		result, err := runCellDef.Handler(args, ctx)
		done <- outcome{result, err}
	}()

	// Wait for the cancel func to be registered, then abort.
	var cancel context.CancelFunc
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cap.mu.Lock()
		cancel = cap.cancelFuncs[cellID]
		cap.mu.Unlock()
		if cancel != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cancel == nil {
		t.Fatal("cancel func was never registered")
	}
	cancel()

	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("cancel must be a result, not a Go error: %v", out.err)
		}
		m := out.result.(map[string]any)
		if m["status"] != "error" {
			t.Fatalf("expected error status after cancel, got %v", m)
		}
		if msg, _ := m["error"].(string); msg != "Query cancelled" {
			t.Fatalf("expected 'Query cancelled', got %q", msg)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("run did not finish after cancel")
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.unsetRunning) != 1 {
		t.Fatalf("UnsetRunning must fire after cancel: %v", cap.unsetRunning)
	}
	if len(cap.deleteCancel) != 1 {
		t.Fatalf("cancel func must be deleted after cancel: %v", cap.deleteCancel)
	}
}
