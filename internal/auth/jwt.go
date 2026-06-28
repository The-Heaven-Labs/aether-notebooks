package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims used by Aether for authentication.
type Claims struct {
	UserID          string `json:"uid"`
	OrgID           string `json:"oid"`
	Role            string `json:"role"`
	IsPlatformAdmin bool   `json:"is_platform_admin,omitempty"`
	jwt.RegisteredClaims
}

// JWTIssuer handles JWT token creation and validation.
type JWTIssuer struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTIssuer creates a new JWTIssuer with the given secret key.
func NewJWTIssuer(secret string, ttl time.Duration) *JWTIssuer {
	return &JWTIssuer{secret: []byte(secret), ttl: ttl}
}

// Issue creates a signed JWT token with the provided claims.
func (j *JWTIssuer) Issue(userID, orgID, role string) (string, error) {
	return j.IssueFull(userID, orgID, role, false)
}

// IssuePlatformAdmin issues a token with IsPlatformAdmin set to true.
func (j *JWTIssuer) IssuePlatformAdmin(userID, orgID, role string) (string, error) {
	return j.IssueFull(userID, orgID, role, true)
}

// IssueFull issues a token with explicit control over the isPlatformAdmin flag.
func (j *JWTIssuer) IssueFull(userID, orgID, role string, isPlatformAdmin bool) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:          userID,
		OrgID:           orgID,
		Role:            role,
		IsPlatformAdmin: isPlatformAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// IssueOnboarding issues a 15-minute token for the post-registration wizard.
// Role="onboarding", no org_id.
func (j *JWTIssuer) IssueOnboarding(userID string, isPlatformAdmin bool) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:          userID,
		OrgID:           "",
		Role:            "onboarding",
		IsPlatformAdmin: isPlatformAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(j.secret)
}

func (j *JWTIssuer) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return j.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}
