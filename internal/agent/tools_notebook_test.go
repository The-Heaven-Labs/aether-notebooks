package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/agent"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/database"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("HNB_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn)
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
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "hnb", Password: "hnb_dev", Database: "hnb"}
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
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "hnb", Password: "hnb_dev", Database: "hnb"}
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
		"notebook_id":   nbID,
		"type":          "code",
		"source":        "SELECT 1",
		"connector_id":  connID,
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
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "hnb", Password: "hnb_dev", Database: "hnb"}
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
		"cell_id":       cellID,
		"connector_id":  connID,
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
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "hnb", Password: "hnb_dev", Database: "hnb"}
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
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "hnb", Password: "hnb_dev", Database: "hnb"}
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

	// Create a cell without position (should go to position 1)
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
	if pos, ok := resultMap["position"].(int); !ok || pos != 1 {
		t.Fatalf("expected position 1, got %v", resultMap["position"])
	}
}