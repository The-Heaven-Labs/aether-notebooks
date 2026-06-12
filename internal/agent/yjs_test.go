package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/agent"
	"github.com/jackc/pgx/v5/pgxpool"
)

func createTestCellForYjs(t *testing.T, pool *pgxpool.Pool, nbID, lang, source string) string {
	t.Helper()
	cellID := uuid.New().String()
	now := time.Now()
	// Use a random position to avoid conflicts
	pos := int(now.UnixNano() % 1000)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
		VALUES ($1, $2, 'code', $3, $4, $5, $6, $6)
	`, cellID, nbID, lang, source, pos, now)
	if err != nil {
		t.Fatalf("create cell: %v", err)
	}
	return cellID
}

func TestUpdateCellInYjs_CreatesValidState(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCellForYjs(t, db.Pool, nbID, "sql", "SELECT 1")

	err := agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cellID, "SELECT 42")
	if err != nil {
		t.Fatalf("UpdateCellInYjs failed: %v", err)
	}

	// Verify state was stored
	var state []byte
	err = db.Pool.QueryRow(context.Background(),
		"SELECT state FROM yjs_documents WHERE notebook_id = $1", nbID,
	).Scan(&state)
	if err != nil {
		t.Fatalf("no yjs state found: %v", err)
	}
	if len(state) == 0 {
		t.Fatal("yjs state is empty")
	}
}

func TestUpdateCellInYjs_PreservesOtherCells(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cell1 := createTestCellForYjs(t, db.Pool, nbID, "sql", "SELECT 1")
	cell2 := createTestCellForYjs(t, db.Pool, nbID, "sql", "SELECT 2")

	// Update cell1
	err := agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cell1, "SELECT 100")
	if err != nil {
		t.Fatalf("UpdateCellInYjs cell1 failed: %v", err)
	}

	// Update cell2
	err = agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cell2, "SELECT 200")
	if err != nil {
		t.Fatalf("UpdateCellInYjs cell2 failed: %v", err)
	}

	// Verify both cells' content is preserved
	doc, err := agent.DecodeYjsState(db.Pool, nbID)
	if err != nil {
		t.Fatalf("DecodeYjsState failed: %v", err)
	}

	ytext1 := doc.GetText("cell:" + cell1)
	if ytext1.ToString() != "SELECT 100" {
		t.Errorf("cell1: expected 'SELECT 100', got '%s'", ytext1.ToString())
	}

	ytext2 := doc.GetText("cell:" + cell2)
	if ytext2.ToString() != "SELECT 200" {
		t.Errorf("cell2: expected 'SELECT 200', got '%s'", ytext2.ToString())
	}
}

func TestUpdateCellInYjs_HandlesEmptyInitialState(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCellForYjs(t, db.Pool, nbID, "sql", "SELECT 1")

	// No yjs_documents row yet — function should create it
	err := agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cellID, "SELECT 99")
	if err != nil {
		t.Fatalf("UpdateCellInYjs failed: %v", err)
	}

	doc, err := agent.DecodeYjsState(db.Pool, nbID)
	if err != nil {
		t.Fatalf("DecodeYjsState failed: %v", err)
	}

	ytext := doc.GetText("cell:" + cellID)
	if ytext.ToString() != "SELECT 99" {
		t.Errorf("expected 'SELECT 99', got '%s'", ytext.ToString())
	}
}

func TestUpdateCellInYjs_SkipsNoopUpdate(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCellForYjs(t, db.Pool, nbID, "sql", "SELECT 1")

	// First update to create the yjs document
	err := agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cellID, "SELECT 1")
	if err != nil {
		t.Fatalf("UpdateCellInYjs failed: %v", err)
	}

	// Get the state timestamp
	var firstState []byte
	db.Pool.QueryRow(context.Background(),
		"SELECT state FROM yjs_documents WHERE notebook_id = $1", nbID,
	).Scan(&firstState)

	// Update with same content — should be a noop
	err = agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cellID, "SELECT 1")
	if err != nil {
		t.Fatalf("UpdateCellInYjs noop failed: %v", err)
	}

	// State should be unchanged
	var secondState []byte
	db.Pool.QueryRow(context.Background(),
		"SELECT state FROM yjs_documents WHERE notebook_id = $1", nbID,
	).Scan(&secondState)

	if len(firstState) != len(secondState) {
		t.Errorf("state changed on noop update: first=%d bytes, second=%d bytes", len(firstState), len(secondState))
	}
}


