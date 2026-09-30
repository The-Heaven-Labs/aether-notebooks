package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/auth"
)

// Sentinel errors mapped to OAuth error codes by the HTTP layer.
var (
	ErrNotFound       = errors.New("oauth: not found")
	ErrReused         = errors.New("oauth: refresh token reuse detected")
	ErrClientMismatch = errors.New("oauth: refresh token was issued to another client")
)

const (
	// AccessTTL keeps access tokens short-lived; harnesses refresh silently.
	AccessTTL = 15 * time.Minute
	// RefreshTTL bounds a consent to 30 days of continued use.
	RefreshTTL = 30 * 24 * time.Hour
	// CodeTTL bounds the authorization code lifetime (spec max: 10 minutes).
	CodeTTL = 60 * time.Second
)

// Client is a dynamically registered public OAuth client (PKCE-only).
type Client struct {
	ID           string
	ClientID     string
	ClientName   string
	RedirectURIs []string
	CreatedAt    time.Time
}

// AuthCode is the stored record behind a single-use authorization code.
type AuthCode struct {
	ClientID        string
	UserID          string
	OrgID           string
	Scopes          []string
	Resource        string
	RedirectURI     string
	CodeChallenge   string
	ChallengeMethod string
}

// TokenRecord is a refresh token row.
type TokenRecord struct {
	ID        string
	FamilyID  string
	ClientID  string
	UserID    string
	OrgID     string
	Scopes    []string
	Resource  string
	ExpiresAt time.Time
	Revoked   bool
	Replaced  bool
}

// Service persists OAuth clients, codes and tokens.
type Service struct {
	pool *pgxpool.Pool
}

// NewService builds a Service over the application pool.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateClient registers a new public client (RFC 7591).
func (s *Service) CreateClient(ctx context.Context, name string, redirectURIs []string) (*Client, error) {
	clientID := "mcp_" + randomToken(16)
	c := &Client{ClientID: clientID, ClientName: name, RedirectURIs: redirectURIs}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO oauth_clients (client_id, client_name, redirect_uris)
		 VALUES ($1, $2, $3) RETURNING id, created_at`,
		clientID, name, redirectURIs).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetClient looks up a registered client by client_id.
func (s *Service) GetClient(ctx context.Context, clientID string) (*Client, error) {
	var c Client
	var uris []string
	err := s.pool.QueryRow(ctx,
		`SELECT id, client_id, client_name, redirect_uris, created_at
		 FROM oauth_clients WHERE client_id = $1`, clientID).
		Scan(&c.ID, &c.ClientID, &c.ClientName, &uris, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.RedirectURIs = uris
	return &c, nil
}

// IssueAuthCode stores a single-use code and returns its plaintext (shown
// once, in the consent redirect).
func (s *Service) IssueAuthCode(ctx context.Context, rec AuthCode) (string, error) {
	code := randomToken(32)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_auth_codes
		     (code_hash, client_id, user_id, org_id, scopes, resource, redirect_uri, code_challenge, challenge_method, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW() + make_interval(secs => $10))`,
		hashToken(code), rec.ClientID, rec.UserID, rec.OrgID, rec.Scopes, rec.Resource,
		rec.RedirectURI, rec.CodeChallenge, rec.ChallengeMethod, CodeTTL.Seconds())
	if err != nil {
		return "", err
	}
	return code, nil
}

// ConsumeAuthCode redeems a code exactly once (atomic UPDATE guards reuse).
func (s *Service) ConsumeAuthCode(ctx context.Context, code string) (*AuthCode, error) {
	var rec AuthCode
	var scopes []string
	err := s.pool.QueryRow(ctx,
		`UPDATE oauth_auth_codes SET used_at = NOW()
		 WHERE code_hash = $1 AND used_at IS NULL AND expires_at > NOW()
		 RETURNING client_id, user_id, org_id, scopes, resource, redirect_uri, code_challenge, challenge_method`,
		hashToken(code)).
		Scan(&rec.ClientID, &rec.UserID, &rec.OrgID, &scopes, &rec.Resource,
			&rec.RedirectURI, &rec.CodeChallenge, &rec.ChallengeMethod)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rec.Scopes = scopes
	return &rec, nil
}

// IssueTokens starts a rotation family: returns the access token and a fresh
// refresh token bound to the identity.
func (s *Service) IssueTokens(ctx context.Context, issuer *auth.JWTIssuer, clientID, userID, orgID, role string, scopes []string, resource string) (access, refresh string, err error) {
	refresh = randomToken(32)
	_, err = s.pool.Exec(ctx,
		`INSERT INTO oauth_tokens
		     (family_id, refresh_hash, client_id, user_id, org_id, scopes, resource, expires_at)
		 VALUES (gen_random_uuid()::text, $1, $2, $3, $4, $5, $6, NOW() + make_interval(secs => $7))`,
		hashToken(refresh), clientID, userID, orgID, scopes, resource, RefreshTTL.Seconds())
	if err != nil {
		return "", "", err
	}
	access, err = issuer.IssueMCPAccessToken(userID, orgID, role, joinScopes(scopes), clientID, resource, AccessTTL)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// RotateRefresh exchanges a refresh token for a new one in the same family.
// The token is bound to the client it was issued to (RFC 6749 §6): presenting
// it from any other client returns ErrClientMismatch. Presenting a superseded
// or revoked token revokes the whole family (reuse detection) and returns
// ErrReused. An unknown or expired token returns ErrNotFound. Membership is
// re-checked so removing a user from the org kills their refresh chains.
func (s *Service) RotateRefresh(ctx context.Context, issuer *auth.JWTIssuer, clientID, refreshToken string) (access, newRefresh string, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rec TokenRecord
	var scopes []string
	err = tx.QueryRow(ctx,
		`SELECT id, family_id, client_id, user_id, org_id, scopes, resource, expires_at,
		        revoked_at IS NOT NULL, replaced_by IS NOT NULL
		 FROM oauth_tokens WHERE refresh_hash = $1 FOR UPDATE`,
		hashToken(refreshToken)).
		Scan(&rec.ID, &rec.FamilyID, &rec.ClientID, &rec.UserID, &rec.OrgID, &scopes, &rec.Resource,
			&rec.ExpiresAt, &rec.Revoked, &rec.Replaced)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}

	// Client binding is checked before the reuse branch so a wrong-client
	// attempt cannot revoke a family it does not own (no DoS on leaked tokens).
	if rec.ClientID != clientID {
		return "", "", ErrClientMismatch
	}

	if rec.Revoked || rec.Replaced {
		// Reuse of a rotated/revoked token: kill the whole family. Failures
		// are propagated — a failed revocation must not be reported as a
		// successful detection.
		if _, err := tx.Exec(ctx,
			`UPDATE oauth_tokens SET revoked_at = NOW()
			 WHERE family_id = $1 AND revoked_at IS NULL`, rec.FamilyID); err != nil {
			return "", "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", "", err
		}
		return "", "", ErrReused
	}
	if !time.Now().Before(rec.ExpiresAt) {
		return "", "", ErrNotFound // simply expired
	}

	var role string
	err = tx.QueryRow(ctx,
		`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`,
		rec.OrgID, rec.UserID).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrNotFound // no longer a member
		}
		return "", "", err
	}

	newRefresh = randomToken(32)
	var newID string
	err = tx.QueryRow(ctx,
		`INSERT INTO oauth_tokens
		     (family_id, refresh_hash, client_id, user_id, org_id, scopes, resource, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW() + make_interval(secs => $8))
		 RETURNING id`,
		rec.FamilyID, hashToken(newRefresh), rec.ClientID, rec.UserID, rec.OrgID, scopes, rec.Resource, RefreshTTL.Seconds()).Scan(&newID)
	if err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(ctx,
		`UPDATE oauth_tokens SET replaced_by = $1 WHERE id = $2`, newID, rec.ID); err != nil {
		return "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}

	access, err = issuer.IssueMCPAccessToken(rec.UserID, rec.OrgID, role, joinScopes(scopes), rec.ClientID, rec.Resource, AccessTTL)
	if err != nil {
		return "", "", err
	}
	return access, newRefresh, nil
}

func joinScopes(scopes []string) string { return strings.Join(scopes, " ") }
