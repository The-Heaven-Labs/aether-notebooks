package api_test

import (
	"context"
	"testing"
)

func TestMigration013Tables(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// sso_providers table exists
	var count int
	err := db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables
         WHERE table_schema='public' AND table_name='sso_providers'`).Scan(&count)
	if err != nil {
		t.Fatalf("query sso_providers: %v", err)
	}
	if count != 1 {
		t.Error("sso_providers table does not exist")
	}

	// org_platform_providers table exists
	err = db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables
         WHERE table_schema='public' AND table_name='org_platform_providers'`).Scan(&count)
	if err != nil {
		t.Fatalf("query org_platform_providers: %v", err)
	}
	if count != 1 {
		t.Error("org_platform_providers table does not exist")
	}

	// orgs table has sso_password_login column
	err = db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns
         WHERE table_schema='public' AND table_name='orgs' AND column_name='sso_password_login'`).Scan(&count)
	if err != nil {
		t.Fatalf("query sso_password_login: %v", err)
	}
	if count != 1 {
		t.Error("sso_password_login column missing from orgs table")
	}
}

func TestMigration013ScopeConstraint(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// platform scope with non-null org_id must be rejected
	_, err := db.Pool.Exec(ctx, `
        INSERT INTO sso_providers (scope, org_id, name, client_id, client_secret_enc, discovery_url)
        VALUES ('platform', gen_random_uuid(), 'test', 'cid', 'sec', 'https://example.com')
    `)
	if err == nil {
		t.Error("expected CHECK violation inserting platform provider with org_id, got nil error")
	}

	// org scope with null org_id must be rejected
	_, err = db.Pool.Exec(ctx, `
        INSERT INTO sso_providers (scope, org_id, name, client_id, client_secret_enc, discovery_url)
        VALUES ('org', NULL, 'test', 'cid', 'sec', 'https://example.com')
    `)
	if err == nil {
		t.Error("expected CHECK violation inserting org provider with null org_id, got nil error")
	}
}

func TestMigration013ScopeValues(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.Pool.Exec(ctx, `
        INSERT INTO sso_providers (scope, name, client_id, client_secret_enc, discovery_url)
        VALUES ('tenant', 'test', 'cid', 'sec', 'https://example.com')
    `)
	if err == nil {
		t.Error("expected CHECK violation for invalid scope value, got nil error")
	}
}
