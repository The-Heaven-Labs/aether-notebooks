package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCClaims struct {
	Subject           string
	Email             string
	Name              string
	Groups            []string
	GroupsUnavailable bool // true when the configured groups source (UserInfo) failed and no fallback groups exist
	// Debug carries the raw IDP payloads observed during the exchange so admins
	// can troubleshoot group-claim mapping. It never contains token strings;
	// callers persisting it must redact with RedactTokenMaterial first.
	Debug *OIDCExchangeDebug
}

// OIDCExchangeDebug is the discarded-otherwise view of one token exchange:
// the full (unredacted) claim maps, the granted scope names, and how the
// configured groups claim was shaped and parsed.
type OIDCExchangeDebug struct {
	GroupsClaim      string
	RawIDTokenClaims map[string]any
	UserInfoClaims   map[string]any
	UserInfoError    string
	GrantedScopes    []string
	// GroupsClaimValue is the raw value found at GroupsClaim on the source that
	// produced ParsedGroups (UserInfo when it supplied groups, else the ID token).
	GroupsClaimValue any
	ParsedGroups     []string
}

type OIDCProvider interface {
	Name() string
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*OIDCClaims, error)
}

type GenericOIDCProvider struct {
	name         string
	verifier     *oidc.IDTokenVerifier
	oauth        oauth2.Config
	groupsClaim  string
	getUserInfo  bool
	oidcProvider *oidc.Provider
}

func NewGenericOIDCProvider(ctx context.Context, name, issuerURL, clientID, clientSecret, redirectURL string, scopes []string, groupsClaim string, getUserInfo bool) (*GenericOIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	if groupsClaim == "" {
		groupsClaim = "groups"
	}

	return &GenericOIDCProvider{
		name:         name,
		verifier:     provider.Verifier(&oidc.Config{ClientID: clientID}),
		oauth:        oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: provider.Endpoint(), RedirectURL: redirectURL, Scopes: scopes},
		groupsClaim:  groupsClaim,
		getUserInfo:  getUserInfo,
		oidcProvider: provider,
	}, nil
}

func (p *GenericOIDCProvider) Name() string {
	return p.name
}

func (p *GenericOIDCProvider) AuthURL(state string) string {
	return p.oauth.AuthCodeURL(state)
}

// parseGroupsClaim extracts string entries from the []any shape the current
// mapping supports. ok reports whether the value had the expected array shape,
// letting callers distinguish "no groups" from "wrong claim type".
func parseGroupsClaim(raw any) (groups []string, ok bool) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	for _, g := range arr {
		if s, ok := g.(string); ok {
			groups = append(groups, s)
		}
	}
	return groups, true
}

func (p *GenericOIDCProvider) Exchange(ctx context.Context, code string) (*OIDCClaims, error) {
	token, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in response")
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}

	var rawClaims map[string]any
	if err := idToken.Claims(&rawClaims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	debug := &OIDCExchangeDebug{
		GroupsClaim:      p.groupsClaim,
		RawIDTokenClaims: rawClaims,
	}
	if scopeRaw, ok := token.Extra("scope").(string); ok {
		debug.GrantedScopes = strings.Fields(scopeRaw)
	}

	claims := &OIDCClaims{
		Subject: idToken.Subject,
		Debug:   debug,
	}

	if email, ok := rawClaims["email"].(string); ok {
		claims.Email = email
	}
	if name, ok := rawClaims["name"].(string); ok {
		claims.Name = name
	}
	if groupsRaw, ok := rawClaims[p.groupsClaim]; ok {
		debug.GroupsClaimValue = groupsRaw
		if groups, shapeOK := parseGroupsClaim(groupsRaw); shapeOK {
			claims.Groups = groups
		}
	}

	if p.getUserInfo && p.oidcProvider != nil {
		userInfoOK := false
		userInfo, err := p.oidcProvider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err != nil {
			debug.UserInfoError = err.Error()
		} else {
			var uiClaims map[string]any
			if err := userInfo.Claims(&uiClaims); err != nil {
				debug.UserInfoError = err.Error()
			} else {
				debug.UserInfoClaims = uiClaims
				userInfoOK = true
				if groupsRaw, ok := uiClaims[p.groupsClaim]; ok {
					debug.GroupsClaimValue = groupsRaw
					if uiGroups, shapeOK := parseGroupsClaim(groupsRaw); shapeOK {
						if len(uiGroups) > 0 {
							claims.Groups = uiGroups
						}
					} else {
						userInfoOK = false
					}
				}
			}
		}
		if !userInfoOK && len(claims.Groups) == 0 {
			claims.GroupsUnavailable = true
		}
	}

	debug.ParsedGroups = claims.Groups
	return claims, nil
}

// tokenMaterialKeys are claim key names whose values may carry credentials or
// token material. Keys are normalized (lowercased, separators stripped) before
// lookup so access_token, accessToken, and access-token all match.
var tokenMaterialKeys = map[string]bool{
	"idtoken":       true,
	"accesstoken":   true,
	"refreshtoken":  true,
	"clientsecret":  true,
	"password":      true,
	"authorization": true,
}

func normalizeClaimKey(key string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(key))
}

// RedactTokenMaterial returns a deep copy of claims with any key that could
// carry token material removed at every depth. Some IDPs embed an access token
// inside ID-token claims, so this pass is mandatory before persisting a
// capture. Non-map values are copied as-is.
func RedactTokenMaterial(claims map[string]any) map[string]any {
	if claims == nil {
		return nil
	}
	out := make(map[string]any, len(claims))
	for k, v := range claims {
		if tokenMaterialKeys[normalizeClaimKey(k)] {
			continue
		}
		out[k] = RedactTokenMaterialValue(v)
	}
	return out
}

// RedactTokenMaterialValue applies RedactTokenMaterial inside slices and maps,
// so a nested groups-claim value is redacted the same way as a top-level claim.
func RedactTokenMaterialValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return RedactTokenMaterial(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = RedactTokenMaterialValue(item)
		}
		return out
	default:
		return value
	}
}
