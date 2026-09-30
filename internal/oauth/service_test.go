package oauth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/database"
)

func setupOAuthTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background()))
	t.Cleanup(func() { db.Close() })
	return db
}

func newOAuthTestService(t *testing.T) (*Service, *database.DB) {
	t.Helper()
	db := setupOAuthTestDB(t)
	return NewService(db.Pool), db
}

func newOAuthTestIssuer(t *testing.T) *auth.JWTIssuer {
	t.Helper()
	return auth.NewJWTIssuer("oauth-service-test-secret", time.Hour)
}

func createOAuthTestIdentity(t *testing.T, db *database.DB) (orgID, userID string) {
	t.Helper()
	ctx := context.Background()
	orgID = uuid.New().String()
	userID = uuid.New().String()
	now := time.Now()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO orgs (id, name, slug, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)
	`, orgID, "OAuth Org "+orgID[:8], "oauth-"+orgID[:8], now)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO users (id, email, name, password_hash, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)
	`, userID, "oauth-"+userID[:8]+"@example.com", "OAuth User", "hash", now)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO org_members (org_id, user_id, role, created_at) VALUES ($1, $2, 'admin', $3)
	`, orgID, userID, now)
	require.NoError(t, err)
	return orgID, userID
}

func registerOAuthTestClient(t *testing.T, svc *Service) *Client {
	t.Helper()
	c, err := svc.CreateClient(context.Background(), "OAuth Test Client",
		[]string{"https://client.example.com/callback"})
	require.NoError(t, err)
	return c
}

func TestCreateClientAndGetClient(t *testing.T) {
	svc, _ := newOAuthTestService(t)
	ctx := context.Background()

	c, err := svc.CreateClient(ctx, "Round Trip Client", []string{"https://client.example.com/cb"})
	require.NoError(t, err)
	require.NotEmpty(t, c.ID)
	require.Contains(t, c.ClientID, "mcp_")
	require.False(t, c.CreatedAt.IsZero())

	got, err := svc.GetClient(ctx, c.ClientID)
	require.NoError(t, err)
	require.Equal(t, c.ID, got.ID)
	require.Equal(t, c.ClientID, got.ClientID)
	require.Equal(t, "Round Trip Client", got.ClientName)
	require.Equal(t, []string{"https://client.example.com/cb"}, got.RedirectURIs)

	_, err = svc.GetClient(ctx, "mcp_unknown_client")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAuthCodeConsumeSingleUse(t *testing.T) {
	svc, db := newOAuthTestService(t)
	ctx := context.Background()
	orgID, userID := createOAuthTestIdentity(t, db)
	client := registerOAuthTestClient(t, svc)

	code, err := svc.IssueAuthCode(ctx, AuthCode{
		ClientID:        client.ClientID,
		UserID:          userID,
		OrgID:           orgID,
		Scopes:          []string{ScopeRead, ScopeQuery},
		Resource:        "https://aether.example.com/mcp",
		RedirectURI:     "https://client.example.com/callback",
		CodeChallenge:   "test-challenge",
		ChallengeMethod: "S256",
	})
	require.NoError(t, err)
	require.NotEmpty(t, code)

	rec, err := svc.ConsumeAuthCode(ctx, code)
	require.NoError(t, err)
	require.Equal(t, client.ClientID, rec.ClientID)
	require.Equal(t, userID, rec.UserID)
	require.Equal(t, orgID, rec.OrgID)
	require.Equal(t, []string{ScopeRead, ScopeQuery}, rec.Scopes)
	require.Equal(t, "https://aether.example.com/mcp", rec.Resource)
	require.Equal(t, "https://client.example.com/callback", rec.RedirectURI)
	require.Equal(t, "test-challenge", rec.CodeChallenge)
	require.Equal(t, "S256", rec.ChallengeMethod)

	_, err = svc.ConsumeAuthCode(ctx, code)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAuthCodeExpired(t *testing.T) {
	svc, db := newOAuthTestService(t)
	ctx := context.Background()
	orgID, userID := createOAuthTestIdentity(t, db)
	client := registerOAuthTestClient(t, svc)

	code, err := svc.IssueAuthCode(ctx, AuthCode{
		ClientID:        client.ClientID,
		UserID:          userID,
		OrgID:           orgID,
		Scopes:          []string{ScopeRead},
		Resource:        "https://aether.example.com/mcp",
		RedirectURI:     "https://client.example.com/callback",
		CodeChallenge:   "test-challenge",
		ChallengeMethod: "S256",
	})
	require.NoError(t, err)

	_, err = db.Pool.Exec(ctx,
		`UPDATE oauth_auth_codes SET expires_at = NOW() - interval '1 minute' WHERE code_hash = $1`,
		hashToken(code))
	require.NoError(t, err)

	_, err = svc.ConsumeAuthCode(ctx, code)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestIssueTokensAndRotateRefresh(t *testing.T) {
	svc, db := newOAuthTestService(t)
	ctx := context.Background()
	issuer := newOAuthTestIssuer(t)
	orgID, userID := createOAuthTestIdentity(t, db)
	client := registerOAuthTestClient(t, svc)

	access, refresh, err := svc.IssueTokens(ctx, issuer, client.ClientID, userID, orgID, "admin",
		[]string{ScopeQuery}, "https://aether.example.com/mcp")
	require.NoError(t, err)
	require.NotEmpty(t, access)
	require.NotEmpty(t, refresh)

	var familyBefore string
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT family_id FROM oauth_tokens WHERE refresh_hash = $1`,
		hashToken(refresh)).Scan(&familyBefore))

	newAccess, newRefresh, err := svc.RotateRefresh(ctx, issuer, refresh)
	require.NoError(t, err)
	require.NotEmpty(t, newAccess)
	require.NotEmpty(t, newRefresh)
	require.NotEqual(t, refresh, newRefresh)

	var familyAfter, replacedBy string
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT family_id, replaced_by FROM oauth_tokens WHERE refresh_hash = $1`,
		hashToken(refresh)).Scan(&familyAfter, &replacedBy))
	require.Equal(t, familyBefore, familyAfter)
	require.NotEmpty(t, replacedBy)

	var newFamily string
	var newRevoked, newReplaced bool
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT family_id, revoked_at IS NOT NULL, replaced_by IS NOT NULL
		 FROM oauth_tokens WHERE refresh_hash = $1`,
		hashToken(newRefresh)).Scan(&newFamily, &newRevoked, &newReplaced))
	require.Equal(t, familyBefore, newFamily)
	require.False(t, newRevoked)
	require.False(t, newReplaced)

	claims, err := issuer.Validate(newAccess)
	require.NoError(t, err)
	require.Equal(t, userID, claims.UserID)
	require.Equal(t, orgID, claims.OrgID)
	require.Equal(t, "admin", claims.Role)
	require.Equal(t, ScopeQuery, claims.Scope)
	require.Equal(t, client.ClientID, claims.ClientID)
}

func TestRotateRefreshReuseRevokesFamily(t *testing.T) {
	svc, db := newOAuthTestService(t)
	ctx := context.Background()
	issuer := newOAuthTestIssuer(t)
	orgID, userID := createOAuthTestIdentity(t, db)
	client := registerOAuthTestClient(t, svc)

	_, refresh, err := svc.IssueTokens(ctx, issuer, client.ClientID, userID, orgID, "admin",
		[]string{ScopeQuery}, "https://aether.example.com/mcp")
	require.NoError(t, err)

	_, rotated, err := svc.RotateRefresh(ctx, issuer, refresh)
	require.NoError(t, err)

	_, _, err = svc.RotateRefresh(ctx, issuer, refresh)
	require.ErrorIs(t, err, ErrReused)

	// A revoked member of the family is also rejected as reuse.
	_, _, err = svc.RotateRefresh(ctx, issuer, rotated)
	require.ErrorIs(t, err, ErrReused)

	var total, revoked int
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE revoked_at IS NOT NULL)
		 FROM oauth_tokens
		 WHERE family_id = (SELECT family_id FROM oauth_tokens WHERE refresh_hash = $1)`,
		hashToken(refresh)).Scan(&total, &revoked))
	require.Equal(t, 2, total)
	require.Equal(t, 2, revoked)
}

func TestRotateRefreshMembershipRemoved(t *testing.T) {
	svc, db := newOAuthTestService(t)
	ctx := context.Background()
	issuer := newOAuthTestIssuer(t)
	orgID, userID := createOAuthTestIdentity(t, db)
	client := registerOAuthTestClient(t, svc)

	_, refresh, err := svc.IssueTokens(ctx, issuer, client.ClientID, userID, orgID, "admin",
		[]string{ScopeQuery}, "https://aether.example.com/mcp")
	require.NoError(t, err)

	_, err = db.Pool.Exec(ctx,
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	require.NoError(t, err)

	_, _, err = svc.RotateRefresh(ctx, issuer, refresh)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRotateRefreshUnknownToken(t *testing.T) {
	svc, _ := newOAuthTestService(t)
	ctx := context.Background()
	issuer := newOAuthTestIssuer(t)

	_, _, err := svc.RotateRefresh(ctx, issuer, "not-a-real-refresh-token")
	require.ErrorIs(t, err, ErrNotFound)
}
