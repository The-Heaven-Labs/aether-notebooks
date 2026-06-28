package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCClaims struct {
	Subject string
	Email   string
	Name    string
	Groups  []string
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

	claims := &OIDCClaims{
		Subject: idToken.Subject,
	}

	if email, ok := rawClaims["email"].(string); ok {
		claims.Email = email
	}
	if name, ok := rawClaims["name"].(string); ok {
		claims.Name = name
	}
	if groupsRaw, ok := rawClaims[p.groupsClaim]; ok {
		if groupsArr, ok := groupsRaw.([]any); ok {
			for _, g := range groupsArr {
				if s, ok := g.(string); ok {
					claims.Groups = append(claims.Groups, s)
				}
			}
		}
	}

	if p.getUserInfo && p.oidcProvider != nil {
		userInfo, err := p.oidcProvider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err == nil {
			var uiClaims map[string]any
			if err := userInfo.Claims(&uiClaims); err == nil {
				if groupsRaw, ok := uiClaims[p.groupsClaim]; ok {
					if groupsArr, ok := groupsRaw.([]any); ok {
						var uiGroups []string
						for _, g := range groupsArr {
							if s, ok := g.(string); ok {
								uiGroups = append(uiGroups, s)
							}
						}
						if len(uiGroups) > 0 {
							claims.Groups = uiGroups
						}
					}
				}
			}
		}
	}

	return claims, nil
}
