package api

import (
	"strings"
)

// normalizePendingEmail canonicalizes an email staged for a not-yet-registered
// user: trimmed, lowercased, required to have exactly one "@" with a
// non-empty local part and domain, and no whitespace. The lowercased form is
// what the pending tables' lower(email) indexes and all lookups use.
func normalizePendingEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if strings.ContainsAny(email, " \t\n\r") {
		return "", false
	}
	local, domain, found := strings.Cut(email, "@")
	if !found || strings.Contains(domain, "@") {
		return "", false
	}
	if local == "" || domain == "" {
		return "", false
	}
	return email, true
}
