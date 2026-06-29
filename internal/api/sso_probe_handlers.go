package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/the-heaven-labs/aether/internal/sso"
)

// @Summary Probe SSO providers
// @Description Returns SSO providers matching the email's domain (unauthenticated, rate-limited)
// @Tags sso
// @Produce json
// @Param email query string true "User email to probe for SSO providers"
// @Success 200 {array} object
// @Failure 429 {object} map[string]string
// @Router /auth/sso-providers [get]
func (s *Server) handleSSOProbe(w http.ResponseWriter, r *http.Request) {
	// Fixed sleep floor to prevent timing-based email enumeration inference.
	defer func() { time.Sleep(5 * time.Millisecond) }()

	ctx := r.Context()

	email := r.URL.Query().Get("email")
	if email == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 || parts[1] == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	domain := parts[1]

	// Rate limit: 20 requests per IP per 60 seconds.
	ip := clientIP(r)
	key := fmt.Sprintf("ratelimit:sso-probe:%s", ip)

	count, err := s.Cache.Client().Incr(ctx, key).Result()
	if err != nil {
		// If Redis is unavailable, allow the request through rather than blocking users.
		count = 1
	} else if count == 1 {
		// New key — set TTL so it expires after 60 seconds.
		s.Cache.Client().Expire(ctx, key, 60*time.Second)
	}

	if count > 20 {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	orgID := OrgIDFromContext(ctx)
	var providers []sso.ProbeResult
	if orgID != "" {
		providers, err = sso.ListProvidersByDomainForOrg(ctx, s.db.Pool, domain, orgID)
	} else {
		providers, err = sso.ListProvidersByDomain(ctx, s.db.Pool, domain)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list providers")
		return
	}

	writeJSON(w, http.StatusOK, providers)
}

// @Summary Registration status
// @Description Returns whether self-registration is disabled (unauthenticated)
// @Tags sso
// @Produce json
// @Success 200 {object} map[string]bool
// @Router /auth/config [get]
func (s *Server) handleRegistrationStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"registration_disabled": s.disableRegistration})
}
