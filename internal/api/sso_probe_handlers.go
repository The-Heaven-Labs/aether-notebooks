package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/heavenlabs/hnb/internal/sso"
)

// handleSSOProbe handles GET /api/v1/auth/sso-providers?email=user@example.com
// It is unauthenticated and returns the list of SSO providers matching the email's domain.
// Rate limited to 20 requests per IP per 60 seconds to prevent enumeration.
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

	providers, err := sso.ListProvidersByDomain(ctx, s.db.Pool, domain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list providers")
		return
	}

	writeJSON(w, http.StatusOK, providers)
}
