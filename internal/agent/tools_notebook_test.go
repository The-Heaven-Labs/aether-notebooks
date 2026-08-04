package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/models"
)

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func setupToolContext(t *testing.T, db *database.DB, orgID, userID, notebookID string) *agent.ToolContext {
	t.Helper()
	return &agent.ToolContext{
		Context:    context.Background(),
		UserID:     userID,
		OrgID:      orgID,
		OrgRole:    "admin",
		NotebookID: notebookID,
		DB:         db.Pool,
		MasterKey:  nil,
	}
}

func createTestOrgAndUser(t *testing.T, pool *pgxpool.Pool) (orgID, userID string) {
	t.Helper()
	orgID = uuid.New().String()
	userID = uuid.New().String()
	now := time.Now()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO orgs (id, name, slug, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)
	`, orgID, "Test Org "+orgID[:8], "slug-"+orgID[:8], now)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO users (id, email, name, password_hash, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)
	`, userID, "test-"+userID[:8]+"@example.com", "Test User", "hash", now)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO org_members (org_id, user_id, role, created_at) VALUES ($1, $2, 'admin', $3)
	`, orgID, userID, now)
	if err != nil {
		t.Fatalf("create org member: %v", err)
	}
	return orgID, userID
}

func createTestNotebook(t *testing.T, pool *pgxpool.Pool, orgID, userID string) string {
	t.Helper()
	nbID := uuid.New().String()
	now := time.Now()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO notebooks (id, org_id, created_by, title, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)
	`, nbID, orgID, userID, "Test Notebook "+nbID[:8], now)
	if err != nil {
		t.Fatalf("create notebook: %v", err)
	}
	return nbID
}

func TestAgentCreateCellWithPosition(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	// Create two existing cells at positions 0 and 1
	now := time.Now()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
		VALUES ($1, $2, 'code', 'sql', 'SELECT 1', 0, $3, $3)
	`, uuid.New().String(), nbID, now)
	if err != nil {
		t.Fatalf("create cell 0: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
		VALUES ($1, $2, 'code', 'sql', 'SELECT 2', 1, $3, $3)
	`, uuid.New().String(), nbID, now)
	if err != nil {
		t.Fatalf("create cell 1: %v", err)
	}

	// Register tools
	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	createCellDef, ok := reg.Get("create_cell")
	if !ok {
		t.Fatalf("create_cell tool not found")
	}
	createCellHandler := createCellDef.Handler

	ctx := setupToolContext(t, db, orgID, userID, nbID)

	// Create a cell at position 1 (should shift existing cell at pos 1 to pos 2)
	args, _ := json.Marshal(map[string]any{
		"notebook_id": nbID,
		"type":        "code",
		"source":      "SELECT 3",
		"position":    1,
	})
	result, err := createCellHandler(args, ctx)
	if err != nil {
		t.Fatalf("create cell with position: %v", err)
	}
	resultMap := result.(map[string]any)
	cellID := resultMap["cell_id"].(string)

	// Verify all cells have unique positions
	rows, err := db.Pool.Query(context.Background(), `SELECT id, position FROM cells WHERE notebook_id = $1 ORDER BY position`, nbID)
	if err != nil {
		t.Fatalf("query cells: %v", err)
	}
	defer rows.Close()

	positions := map[int]bool{}
	count := 0
	for rows.Next() {
		var id string
		var pos int
		if err := rows.Scan(&id, &pos); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if positions[pos] {
			t.Fatalf("duplicate position %d (cell %s)", pos, id)
		}
		positions[pos] = true
		count++
	}
	if count != 3 {
		t.Fatalf("expected 3 cells, got %d", count)
	}

	t.Logf("created cell %s with position shifting, all positions unique", cellID)
}

func TestAgentRunCellWithLimitQuotedColumn(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	// Create a Postgres connector for this org
	connID := uuid.New().String()
	now := time.Now()
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "aether", Password: "aether_dev", Database: "aether"}
	cfgJSON, _ := json.Marshal(cfg)
	masterKey := crypto.DeriveKey("test-master-key-for-tests-only!")
	configEncrypted, err := crypto.Encrypt(cfgJSON, masterKey)
	if err != nil {
		t.Fatalf("encrypt config: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, created_at, updated_at)
		VALUES ($1, $2, 'Test PG', 'postgres', $3, $4, $5, $5)
	`, connID, orgID, configEncrypted, userID, now)
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}

	// Create a cell with a trailing semicolon and newline in source
	// This tests that the "limit" column is properly quoted in SQL queries
	cellID := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, connector_id, source, position, "limit", created_at, updated_at)
		VALUES ($1, $2, 'code', 'sql', $3, $4, 0, 1000, $5, $5)
	`, cellID, nbID, connID, "SELECT 1 AS x;\n", now)
	if err != nil {
		t.Fatalf("create cell: %v", err)
	}

	// Register tools and test the run_cell handler which queries the "limit" column
	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	runCellDef, ok := reg.Get("run_cell")
	if !ok {
		t.Fatalf("run_cell tool not found")
	}
	runCellHandler := runCellDef.Handler

	// Set up tool context with master key so run_cell can decrypt connector config
	ctx := setupToolContext(t, db, orgID, userID, nbID)
	ctx.MasterKey = masterKey

	args, _ := json.Marshal(map[string]any{
		"cell_id": cellID,
	})
	result, err := runCellHandler(args, ctx)
	if err != nil {
		t.Fatalf("run cell: %v", err)
	}

	// The result should have cell_id and status fields (not an error about "limit" syntax)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if resultMap["cell_id"] == nil {
		t.Fatalf("expected cell_id in result, got %v", resultMap)
	}
	t.Logf("run_cell succeeded with properly quoted 'limit' column: %v", resultMap)
}

func TestAgentCreateCellWithConnectorID(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	connID := uuid.New().String()
	now := time.Now()
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "aether", Password: "aether_dev", Database: "aether"}
	cfgJSON, _ := json.Marshal(cfg)
	masterKey := crypto.DeriveKey("test-master-key-for-tests-only!")
	configEncrypted, err := crypto.Encrypt(cfgJSON, masterKey)
	if err != nil {
		t.Fatalf("encrypt config: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, created_at, updated_at)
		VALUES ($1, $2, 'Test PG', 'postgres', $3, $4, $5, $5)
	`, connID, orgID, configEncrypted, userID, now)
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	createCellDef, _ := reg.Get("create_cell")
	createCellHandler := createCellDef.Handler

	ctx := setupToolContext(t, db, orgID, userID, nbID)

	args, _ := json.Marshal(map[string]any{
		"notebook_id":  nbID,
		"type":         "code",
		"source":       "SELECT 1",
		"connector_id": connID,
	})
	result, err := createCellHandler(args, ctx)
	if err != nil {
		t.Fatalf("create cell with connector_id: %v", err)
	}

	cellID := result.(map[string]any)["cell_id"].(string)

	var gotConnID *string
	err = db.Pool.QueryRow(context.Background(), `SELECT connector_id FROM cells WHERE id = $1`, cellID).Scan(&gotConnID)
	if err != nil {
		t.Fatalf("query cell connector_id: %v", err)
	}
	if gotConnID == nil || *gotConnID != connID {
		t.Fatalf("expected connector_id %s, got %v", connID, gotConnID)
	}
	t.Logf("create_cell with connector_id correctly saved: %s", connID)
}

func TestAgentUpdateCellConnectorID(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	connID := uuid.New().String()
	now := time.Now()
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "aether", Password: "aether_dev", Database: "aether"}
	cfgJSON, _ := json.Marshal(cfg)
	masterKey := crypto.DeriveKey("test-master-key-for-tests-only!")
	configEncrypted, err := crypto.Encrypt(cfgJSON, masterKey)
	if err != nil {
		t.Fatalf("encrypt config: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, created_at, updated_at)
		VALUES ($1, $2, 'Test PG', 'postgres', $3, $4, $5, $5)
	`, connID, orgID, configEncrypted, userID, now)
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	createCellDef, _ := reg.Get("create_cell")
	updateCellDef, _ := reg.Get("update_cell")

	ctx := setupToolContext(t, db, orgID, userID, nbID)

	createArgs, _ := json.Marshal(map[string]any{
		"notebook_id": nbID,
		"type":        "code",
		"source":      "SELECT 1",
	})
	createResult, err := createCellDef.Handler(createArgs, ctx)
	if err != nil {
		t.Fatalf("create cell: %v", err)
	}
	cellID := createResult.(map[string]any)["cell_id"].(string)

	updateArgs, _ := json.Marshal(map[string]any{
		"cell_id":      cellID,
		"connector_id": connID,
	})
	_, err = updateCellDef.Handler(updateArgs, ctx)
	if err != nil {
		t.Fatalf("update cell with connector_id: %v", err)
	}

	var gotConnID *string
	err = db.Pool.QueryRow(context.Background(), `SELECT connector_id FROM cells WHERE id = $1`, cellID).Scan(&gotConnID)
	if err != nil {
		t.Fatalf("query cell connector_id: %v", err)
	}
	if gotConnID == nil || *gotConnID != connID {
		t.Fatalf("expected connector_id %s after update, got %v", connID, gotConnID)
	}
	t.Logf("update_cell with connector_id correctly saved: %s", connID)
}

func TestAgentRunCellNotebookConnectorFallback(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	connID := uuid.New().String()
	now := time.Now()
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "aether", Password: "aether_dev", Database: "aether"}
	cfgJSON, _ := json.Marshal(cfg)
	masterKey := crypto.DeriveKey("test-master-key-for-tests-only!")
	configEncrypted, err := crypto.Encrypt(cfgJSON, masterKey)
	if err != nil {
		t.Fatalf("encrypt config: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, created_at, updated_at)
		VALUES ($1, $2, 'Test PG', 'postgres', $3, $4, $5, $5)
	`, connID, orgID, configEncrypted, userID, now)
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}

	// Set the connector on the notebook level (not on the cell)
	_, err = db.Pool.Exec(context.Background(), `
		UPDATE notebooks SET connector_id = $1 WHERE id = $2
	`, connID, nbID)
	if err != nil {
		t.Fatalf("update notebook connector: %v", err)
	}

	// Create a cell WITHOUT a connector_id (should fall back to notebook's connector)
	cellID := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
		VALUES ($1, $2, 'code', 'sql', 'SELECT 1 AS fallback_test', 0, $3, $3)
	`, cellID, nbID, now)
	if err != nil {
		t.Fatalf("create cell: %v", err)
	}

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	runCellDef, _ := reg.Get("run_cell")

	ctx := setupToolContext(t, db, orgID, userID, nbID)
	ctx.MasterKey = masterKey

	args, _ := json.Marshal(map[string]any{
		"cell_id": cellID,
	})
	result, err := runCellDef.Handler(args, ctx)
	if err != nil {
		t.Fatalf("run cell with notebook connector fallback: %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["status"] != "completed" {
		t.Fatalf("expected status=completed, got %v", resultMap)
	}
	t.Logf("run_cell correctly fell back to notebook connector: %v", resultMap)
}

func TestAgentRunCellPersistsOutputs(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	connID := uuid.New().String()
	now := time.Now()
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "aether", Password: "aether_dev", Database: "aether"}
	cfgJSON, _ := json.Marshal(cfg)
	masterKey := crypto.DeriveKey("test-master-key-for-tests-only!")
	configEncrypted, err := crypto.Encrypt(cfgJSON, masterKey)
	if err != nil {
		t.Fatalf("encrypt config: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, created_at, updated_at)
		VALUES ($1, $2, 'Test PG', 'postgres', $3, $4, $5, $5)
	`, connID, orgID, configEncrypted, userID, now)
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}

	cellID := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, connector_id, source, position, created_at, updated_at)
		VALUES ($1, $2, 'code', 'sql', $3, 'SELECT 1 AS output_test', 0, $4, $4)
	`, cellID, nbID, connID, now)
	if err != nil {
		t.Fatalf("create cell: %v", err)
	}

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	runCellDef, _ := reg.Get("run_cell")

	ctx := setupToolContext(t, db, orgID, userID, nbID)
	ctx.MasterKey = masterKey

	args, _ := json.Marshal(map[string]any{"cell_id": cellID})
	result, err := runCellDef.Handler(args, ctx)
	if err != nil {
		t.Fatalf("run cell: %v", err)
	}
	resultMap := result.(map[string]any)
	if resultMap["status"] != "completed" {
		t.Fatalf("expected status=completed, got %v", resultMap)
	}

	// Verify outputs were persisted to the database
	var outputs []byte
	err = db.Pool.QueryRow(context.Background(), `SELECT outputs FROM cells WHERE id = $1`, cellID).Scan(&outputs)
	if err != nil {
		t.Fatalf("query outputs: %v", err)
	}
	if len(outputs) == 0 || string(outputs) == "[]" || string(outputs) == "null" {
		t.Fatalf("expected outputs to be persisted, got: %s", string(outputs))
	}

	var parsed []map[string]any
	if err := json.Unmarshal(outputs, &parsed); err != nil {
		t.Fatalf("parse outputs: %v", err)
	}
	if len(parsed) == 0 {
		t.Fatalf("expected at least one output entry, got %d", len(parsed))
	}
	if parsed[0]["type"] != "table" {
		t.Fatalf("expected output type 'table', got %v", parsed[0]["type"])
	}
	t.Logf("run_cell persisted outputs correctly: type=%s", parsed[0]["type"])
}

func TestAgentCreateCellAtEnd(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	// Create one existing cell
	now := time.Now()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
		VALUES ($1, $2, 'code', 'sql', 'SELECT 1', 0, $3, $3)
	`, uuid.New().String(), nbID, now)
	if err != nil {
		t.Fatalf("create cell: %v", err)
	}

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	createCellDef, ok := reg.Get("create_cell")
	if !ok {
		t.Fatalf("create_cell tool not found")
	}
	createCellHandler := createCellDef.Handler
	ctx := setupToolContext(t, db, orgID, userID, nbID)

	// Create a cell without position (should go to position 2, 1-indexed)
	args, _ := json.Marshal(map[string]any{
		"notebook_id": nbID,
		"type":        "code",
		"source":      "SELECT 2",
	})
	result, err := createCellHandler(args, ctx)
	if err != nil {
		t.Fatalf("create cell at end: %v", err)
	}
	resultMap := result.(map[string]any)
	if pos, ok := resultMap["position"].(int); !ok || pos != 2 {
		t.Fatalf("expected position 2, got %v", resultMap["position"])
	}
}

func TestAgentMoveCell(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	createCellDef, _ := reg.Get("create_cell")
	moveCellDef, _ := reg.Get("move_cell")
	createCellHandler := createCellDef.Handler
	moveCellHandler := moveCellDef.Handler
	ctx := setupToolContext(t, db, orgID, userID, nbID)

	// Create 25 cells like the agent does
	var firstCellID string
	for i := 0; i < 25; i++ {
		args, _ := json.Marshal(map[string]any{
			"notebook_id": nbID,
			"type":        "code",
			"source":      "SELECT " + fmt.Sprint(i),
			"position":    i,
		})
		result, err := createCellHandler(args, ctx)
		if err != nil {
			t.Fatalf("create cell %d: %v", i, err)
		}
		if i == 0 {
			firstCellID = result.(map[string]any)["cell_id"].(string)
		}
	}

	// Verify 25 cells with contiguous positions
	verifyPositions(t, db.Pool, nbID, 25)

	// Move first-created cell (ends up at last position due to shifting) to position 1
	args1, _ := json.Marshal(map[string]any{
		"cell_id":      firstCellID,
		"new_position": 1,
	})
	_, err := moveCellHandler(args1, ctx)
	if err != nil {
		t.Fatalf("move cell to position 1: %v", err)
	}
	verifyPositions(t, db.Pool, nbID, 25)

	// Move it back to the end
	args2, _ := json.Marshal(map[string]any{
		"cell_id":      firstCellID,
		"new_position": 25,
	})
	_, err = moveCellHandler(args2, ctx)
	if err != nil {
		t.Fatalf("move cell to position 25: %v", err)
	}
	verifyPositions(t, db.Pool, nbID, 25)

	// Move middle cell
	args3, _ := json.Marshal(map[string]any{
		"cell_id":      firstCellID,
		"new_position": 13,
	})
	_, err = moveCellHandler(args3, ctx)
	if err != nil {
		t.Fatalf("move cell to position 13: %v", err)
	}
	verifyPositions(t, db.Pool, nbID, 25)
}

func verifyPositions(t *testing.T, pool *pgxpool.Pool, nbID string, expectedCount int) {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT position FROM cells WHERE notebook_id = $1 ORDER BY position`, nbID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	seen := map[int]bool{}
	var count int
	for rows.Next() {
		var pos int
		if err := rows.Scan(&pos); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if seen[pos] {
			t.Fatalf("DUPLICATE position %d", pos)
		}
		seen[pos] = true
		count++
	}
	if count != expectedCount {
		t.Fatalf("expected %d cells, got %d", expectedCount, count)
	}
}

func TestAgentSwapCells(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	// Create 3 cells at positions 0, 1, 2
	now := time.Now()
	cellIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		cellIDs[i] = uuid.New().String()
		_, err := db.Pool.Exec(context.Background(), `
			INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
			VALUES ($1, $2, 'code', 'sql', 'SELECT 1', $3, $4, $4)
		`, cellIDs[i], nbID, i, now)
		if err != nil {
			t.Fatalf("create cell %d: %v", i, err)
		}
	}

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	swapDef, ok := reg.Get("swap_cells")
	if !ok {
		t.Fatalf("swap_cells tool not found")
	}
	handler := swapDef.Handler
	ctx := setupToolContext(t, db, orgID, userID, nbID)

	// Swap cells at positions 0 and 2
	args, _ := json.Marshal(map[string]any{
		"cell_id_a": cellIDs[0],
		"cell_id_b": cellIDs[2],
	})
	_, err := handler(args, ctx)
	if err != nil {
		t.Fatalf("swap cells: %v", err)
	}

	// Verify swapped positions
	var pos0, pos2 int
	if err := db.Pool.QueryRow(context.Background(), `SELECT position FROM cells WHERE id=$1`, cellIDs[0]).Scan(&pos0); err != nil {
		t.Fatalf("get pos0: %v", err)
	}
	if err := db.Pool.QueryRow(context.Background(), `SELECT position FROM cells WHERE id=$1`, cellIDs[2]).Scan(&pos2); err != nil {
		t.Fatalf("get pos2: %v", err)
	}
	if pos0 != 2 || pos2 != 0 {
		t.Fatalf("expected positions 2 and 0, got %d and %d", pos0, pos2)
	}

	// Verify no duplicates
	verifyPositions(t, db.Pool, nbID, 3)
}

func TestAgentCreateNotebookSeedsACLForUser(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)

	// Create a second user (editor) who should NOT have access
	editorID := uuid.New().String()
	now := time.Now()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO users (id, email, name, password_hash, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)
	`, editorID, "editor-"+uuid.New().String()[:8]+"@example.com", "Editor", "hash", now)
	if err != nil {
		t.Fatalf("create editor: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO org_members (org_id, user_id, role, created_at) VALUES ($1, $2, 'editor', $3)
	`, orgID, editorID, now)
	if err != nil {
		t.Fatalf("create editor member: %v", err)
	}

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	createNotebookDef, ok := reg.Get("create_notebook")
	if !ok {
		t.Fatalf("create_notebook tool not found")
	}

	// Use non-admin context for the creating user
	ctx := &agent.ToolContext{
		Context:   context.Background(),
		UserID:    userID,
		OrgID:     orgID,
		OrgRole:   "editor",
		DB:        db.Pool,
		MasterKey: nil,
	}

	args, _ := json.Marshal(map[string]any{
		"title": "ACL Test Notebook",
	})
	result, err := createNotebookDef.Handler(args, ctx)
	if err != nil {
		t.Fatalf("create notebook: %v", err)
	}
	nbID := result.(map[string]any)["notebook_id"].(string)

	// Verify ACL entry exists for the creator
	var aclCount int
	err = db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM acl_entries
		WHERE resource_type = 'notebook' AND resource_id = $1::uuid
		AND subject_type = 'user' AND subject_id = $2
	`, nbID, userID).Scan(&aclCount)
	if err != nil {
		t.Fatalf("query ACL: %v", err)
	}
	if aclCount != 1 {
		t.Fatalf("expected 1 ACL entry for creator, got %d", aclCount)
	}

	// Verify the editor does NOT have an ACL entry
	var editorACLCount int
	err = db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM acl_entries
		WHERE resource_type = 'notebook' AND resource_id = $1::uuid
		AND subject_type = 'user' AND subject_id = $2
	`, nbID, editorID).Scan(&editorACLCount)
	if err != nil {
		t.Fatalf("query editor ACL: %v", err)
	}
	if editorACLCount != 0 {
		t.Fatalf("expected 0 ACL entries for non-creator, got %d", editorACLCount)
	}

	t.Logf("notebook %s seeded ACL correctly for user %s", nbID, userID)
}

func TestAgentCreateSkillSeedsACLForUser(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)

	reg := agent.NewToolRegistry()
	agent.RegisterAgentTools(reg, db.Pool, nil)
	createSkillDef, ok := reg.Get("create_skill")
	if !ok {
		t.Fatalf("create_skill tool not found")
	}

	// Insert a minimal agent + session so the skill handler's SQL subquery works
	agentID := uuid.New().String()
	sessionID := uuid.New().String()
	now := time.Now()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO agents (id, org_id, name, created_by, created_at, updated_at)
		VALUES ($1, $2, 'test-agent', $3, $4, $4)
	`, agentID, orgID, userID, now)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO agent_sessions (id, agent_id, user_id, created_at)
		VALUES ($1, $2, $3, $4)
	`, sessionID, agentID, userID, now)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	ctx := &agent.ToolContext{
		Context:   context.Background(),
		UserID:    userID,
		OrgID:     orgID,
		OrgRole:   "editor",
		SessionID: sessionID,
		DB:        db.Pool,
		MasterKey: nil,
	}

	args, _ := json.Marshal(map[string]any{
		"name":          "test-skill",
		"system_prompt": "You are a test skill.",
	})
	result, err := createSkillDef.Handler(args, ctx)
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	skillID := result.(map[string]any)["skill_id"].(string)

	// Verify ACL entry exists for the creator
	var aclCount int
	err = db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM acl_entries
		WHERE resource_type = 'skill' AND resource_id = $1
		AND subject_type = 'user' AND subject_id = $2
	`, skillID, userID).Scan(&aclCount)
	if err != nil {
		t.Fatalf("query ACL: %v", err)
	}
	if aclCount != 1 {
		t.Fatalf("expected 1 ACL entry for creator, got %d", aclCount)
	}

	t.Logf("skill %s seeded ACL correctly for user %s", skillID, userID)
}

func TestAgentCreateNotebookInFolderWithoutPermissionDenied(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)

	// Create a folder owned by another user
	otherUserID := uuid.New().String()
	now := time.Now()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO users (id, email, name, password_hash, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)
	`, otherUserID, "other-"+uuid.New().String()[:8]+"@example.com", "Other", "hash", now)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO org_members (org_id, user_id, role, created_at) VALUES ($1, $2, 'editor', $3)
	`, orgID, otherUserID, now)
	if err != nil {
		t.Fatalf("create other member: %v", err)
	}

	folderID := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO folders (id, org_id, name, created_by, created_at, updated_at)
		VALUES ($1, $2, 'restricted', $3, $4, $4)
	`, folderID, orgID, otherUserID, now)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	// Seed ACL so only otherUserID can edit this folder
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'folder', $2::uuid, 'user', $3, ARRAY['view','edit','delete'])
	`, orgID, folderID, otherUserID)
	if err != nil {
		t.Fatalf("seed folder ACL: %v", err)
	}

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	createNotebookDef, ok := reg.Get("create_notebook")
	if !ok {
		t.Fatalf("create_notebook tool not found")
	}

	ctx := &agent.ToolContext{
		Context:   context.Background(),
		UserID:    userID,
		OrgID:     orgID,
		OrgRole:   "editor",
		DB:        db.Pool,
		MasterKey: nil,
	}

	args, _ := json.Marshal(map[string]any{
		"title":     "Should Fail",
		"folder_id": folderID,
	})
	_, err = createNotebookDef.Handler(args, ctx)
	if err == nil {
		t.Fatalf("expected error when creating notebook in folder without permission, got nil")
	}
	t.Logf("correctly denied notebook creation in restricted folder: %v", err)
}

func TestAgentListNotebookParametersEmpty(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	listDef, ok := reg.Get("list_notebook_parameters")
	if !ok {
		t.Fatalf("list_notebook_parameters tool not found")
	}
	ctx := setupToolContext(t, db, orgID, userID, nbID)

	args, _ := json.Marshal(map[string]any{"notebook_id": nbID})
	result, err := listDef.Handler(args, ctx)
	if err != nil {
		t.Fatalf("list parameters: %v", err)
	}
	resultMap := result.(map[string]any)
	if resultMap["count"].(int) != 0 {
		t.Fatalf("expected count 0, got %v", resultMap["count"])
	}
	params, ok := resultMap["parameters"].([]models.Parameter)
	if !ok || len(params) != 0 {
		t.Fatalf("expected empty parameters, got %v", resultMap["parameters"])
	}
	t.Logf("list_notebook_parameters correctly returned an empty list")
}

func TestAgentSetAndListNotebookParameters(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	listDef, _ := reg.Get("list_notebook_parameters")
	setDef, _ := reg.Get("set_notebook_parameters")
	ctx := setupToolContext(t, db, orgID, userID, nbID)

	// Set two parameters (mimics the agent's read-modify-write pattern)
	setArgs, _ := json.Marshal(map[string]any{
		"notebook_id": nbID,
		"parameters": []map[string]any{
			{"name": "start_date", "type": "date", "default": "2026-01-01"},
			{"name": "region", "type": "string", "default": "BR"},
		},
	})
	result, err := setDef.Handler(setArgs, ctx)
	if err != nil {
		t.Fatalf("set parameters: %v", err)
	}
	resultMap := result.(map[string]any)
	if resultMap["status"] != "updated" {
		t.Fatalf("expected status=updated, got %v", resultMap)
	}
	if resultMap["count"].(int) != 2 {
		t.Fatalf("expected count 2, got %v", resultMap["count"])
	}

	// Verify persisted in DB
	var paramsJSON []byte
	if err := db.Pool.QueryRow(context.Background(), `SELECT parameters FROM notebooks WHERE id = $1`, nbID).Scan(&paramsJSON); err != nil {
		t.Fatalf("query parameters: %v", err)
	}
	var stored []models.Parameter
	if err := json.Unmarshal(paramsJSON, &stored); err != nil {
		t.Fatalf("parse parameters: %v", err)
	}
	if len(stored) != 2 || stored[0].Name != "start_date" || stored[0].Default != "2026-01-01" {
		t.Fatalf("unexpected stored parameters: %+v", stored)
	}

	// List should return them
	listArgs, _ := json.Marshal(map[string]any{"notebook_id": nbID})
	listResult, err := listDef.Handler(listArgs, ctx)
	if err != nil {
		t.Fatalf("list parameters: %v", err)
	}
	listMap := listResult.(map[string]any)
	if listMap["count"].(int) != 2 {
		t.Fatalf("expected listed count 2, got %v", listMap["count"])
	}

	// Update an existing default + delete one (atomic replace pattern)
	setArgs2, _ := json.Marshal(map[string]any{
		"notebook_id": nbID,
		"parameters": []map[string]any{
			{"name": "start_date", "type": "date", "default": "2026-06-01"},
		},
	})
	if _, err := setDef.Handler(setArgs2, ctx); err != nil {
		t.Fatalf("set parameters (replace): %v", err)
	}

	listResult2, err := listDef.Handler(listArgs, ctx)
	if err != nil {
		t.Fatalf("list parameters after replace: %v", err)
	}
	listMap2 := listResult2.(map[string]any)
	if listMap2["count"].(int) != 1 {
		t.Fatalf("expected count 1 after replace, got %v", listMap2["count"])
	}
	listed := listMap2["parameters"].([]models.Parameter)
	if listed[0].Name != "start_date" || listed[0].Default != "2026-06-01" {
		t.Fatalf("expected updated start_date default, got %+v", listed[0])
	}
	t.Logf("set/list notebook parameters round-trip verified")
}

func TestAgentSetNotebookParametersDefaultsToContextNotebook(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	listDef, _ := reg.Get("list_notebook_parameters")
	setDef, _ := reg.Get("set_notebook_parameters")
	ctx := setupToolContext(t, db, orgID, userID, nbID)

	// Omit notebook_id — should default to ctx.NotebookID
	setArgs, _ := json.Marshal(map[string]any{
		"parameters": []map[string]any{
			{"name": "limit", "type": "number", "default": "100"},
		},
	})
	if _, err := setDef.Handler(setArgs, ctx); err != nil {
		t.Fatalf("set parameters without notebook_id: %v", err)
	}

	listArgs, _ := json.Marshal(map[string]any{})
	listResult, err := listDef.Handler(listArgs, ctx)
	if err != nil {
		t.Fatalf("list parameters without notebook_id: %v", err)
	}
	listMap := listResult.(map[string]any)
	if listMap["count"].(int) != 1 {
		t.Fatalf("expected count 1, got %v", listMap["count"])
	}
	t.Logf("parameter tools correctly defaulted to the context notebook")
}

func TestAgentSetNotebookParametersRequiresNotebookID(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	setDef, _ := reg.Get("set_notebook_parameters")

	// No ctx.NotebookID and no notebook_id arg
	ctx := setupToolContext(t, db, orgID, userID, "")
	args, _ := json.Marshal(map[string]any{
		"parameters": []map[string]any{{"name": "x", "type": "string"}},
	})
	if _, err := setDef.Handler(args, ctx); err == nil {
		t.Fatalf("expected error when notebook_id is missing, got nil")
	}
	t.Logf("set_notebook_parameters correctly requires notebook_id")
}

func TestAgentSetNotebookParametersValidation(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	setDef, _ := reg.Get("set_notebook_parameters")
	ctx := setupToolContext(t, db, orgID, userID, nbID)

	// Duplicate names should be rejected
	dupArgs, _ := json.Marshal(map[string]any{
		"notebook_id": nbID,
		"parameters": []map[string]any{
			{"name": "region", "type": "string"},
			{"name": "region", "type": "string"},
		},
	})
	if _, err := setDef.Handler(dupArgs, ctx); err == nil {
		t.Fatalf("expected error for duplicate parameter names, got nil")
	}

	// Empty names should be rejected
	emptyArgs, _ := json.Marshal(map[string]any{
		"notebook_id": nbID,
		"parameters":  []map[string]any{{"name": "", "type": "string"}},
	})
	if _, err := setDef.Handler(emptyArgs, ctx); err == nil {
		t.Fatalf("expected error for empty parameter name, got nil")
	}
	t.Logf("set_notebook_parameters correctly validates parameter names")
}

func TestAgentSetNotebookParametersOrgIsolation(t *testing.T) {
	db := setupTestDB(t)
	orgA, userA := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgA, userA)

	// User in a different org
	orgB, userB := createTestOrgAndUser(t, db.Pool)

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	setDef, _ := reg.Get("set_notebook_parameters")

	// Non-admin ctx from org B trying to set params on org A's notebook
	ctx := &agent.ToolContext{
		Context: context.Background(),
		UserID:  userB,
		OrgID:   orgB,
		OrgRole: "editor",
		DB:      db.Pool,
	}
	args, _ := json.Marshal(map[string]any{
		"notebook_id": nbID,
		"parameters":  []map[string]any{{"name": "hacked", "type": "string"}},
	})
	if _, err := setDef.Handler(args, ctx); err == nil {
		t.Fatalf("expected cross-org set to fail, got nil")
	}

	// Admin ctx from org B bypasses ACL but the org-scoped UPDATE must still fail
	adminCtx := &agent.ToolContext{
		Context: context.Background(),
		UserID:  userB,
		OrgID:   orgB,
		OrgRole: "admin",
		DB:      db.Pool,
	}
	if _, err := setDef.Handler(args, adminCtx); err == nil {
		t.Fatalf("expected cross-org set with admin role to fail, got nil")
	}

	// Verify org A's notebook was not modified
	var paramsJSON []byte
	if err := db.Pool.QueryRow(context.Background(), `SELECT parameters FROM notebooks WHERE id = $1`, nbID).Scan(&paramsJSON); err != nil {
		t.Fatalf("query parameters: %v", err)
	}
	var stored []models.Parameter
	json.Unmarshal(paramsJSON, &stored)
	if len(stored) != 0 {
		t.Fatalf("expected no parameters stored, got %+v", stored)
	}
	t.Logf("cross-org parameter writes correctly blocked")
}

func TestSeedBuiltinToolsIncludesParameterTools(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)

	agent.SeedBuiltinTools(context.Background(), db.Pool, orgID)

	var count int
	err := db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM tools
		WHERE org_id = $1 AND type = 'builtin'
		  AND name IN ('list_notebook_parameters', 'set_notebook_parameters')
	`, orgID).Scan(&count)
	if err != nil {
		t.Fatalf("query tools: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 parameter tools seeded, got %d", count)
	}

	// Running the seed again must be idempotent (ON CONFLICT DO NOTHING)
	var countBefore int
	db.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM tools WHERE org_id = $1 AND type = 'builtin'`, orgID).Scan(&countBefore)
	agent.SeedBuiltinTools(context.Background(), db.Pool, orgID)
	var countAfter int
	db.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM tools WHERE org_id = $1 AND type = 'builtin'`, orgID).Scan(&countAfter)
	if countAfter != countBefore {
		t.Fatalf("seed is not idempotent: %d before, %d after", countBefore, countAfter)
	}
	t.Logf("SeedBuiltinTools includes parameter tools and is idempotent (user %s)", userID)
}
