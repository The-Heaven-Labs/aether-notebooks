package audit_test

import (
	"context"
	"os"
	"testing"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/database"
)

func TestLogAndQuery(t *testing.T) {
	dsn := os.Getenv("HNB_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"
	}

	db, err := database.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	// Seed an org and user to satisfy FK constraints
	var orgID, userID string
	err = db.Pool.QueryRow(context.Background(),
		`INSERT INTO orgs (name, slug) VALUES ('audit-test-org', 'audit-test-org-'||gen_random_uuid()) RETURNING id`,
	).Scan(&orgID)
	if err != nil {
		t.Fatalf("seed org: %v", err)
	}
	err = db.Pool.QueryRow(context.Background(),
		`INSERT INTO users (email, name) VALUES ('audittest-'||gen_random_uuid()||'@example.com', 'Audit Test') RETURNING id`,
	).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(context.Background(), `DELETE FROM orgs WHERE id = $1`, orgID)
		db.Pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	logger := audit.NewLogger(db)

	err = logger.Log(context.Background(), audit.Entry{
		OrgID:        orgID,
		UserID:       userID,
		Action:       "notebook.create",
		ResourceType: "notebook",
	})
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	entries, err := logger.Query(context.Background(), audit.QueryParams{
		OrgID: orgID,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}
	if entries[0].Action != "notebook.create" {
		t.Fatalf("expected notebook.create, got %s", entries[0].Action)
	}
}
