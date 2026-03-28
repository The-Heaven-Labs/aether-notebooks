package api

import (
	"net/http"
	"time"
)

func (s *Server) handleAdminListOrgs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.Pool.Query(ctx,
		`SELECT o.id, o.name, o.slug, COUNT(m.user_id) as member_count, o.created_at
         FROM orgs o
         LEFT JOIN org_members m ON m.org_id = o.id
         GROUP BY o.id ORDER BY o.created_at DESC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type orgSummary struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Slug        string    `json:"slug"`
		MemberCount int       `json:"member_count"`
		CreatedAt   time.Time `json:"created_at"`
	}
	var orgs []orgSummary
	for rows.Next() {
		var o orgSummary
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.MemberCount, &o.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		orgs = append(orgs, o)
	}
	if orgs == nil {
		orgs = []orgSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"orgs": orgs})
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.Pool.Query(ctx,
		`SELECT u.id, u.email, u.name, u.is_platform_admin, u.created_at,
                COALESCE(array_agg(o.name ORDER BY o.name) FILTER (WHERE o.name IS NOT NULL), ARRAY[]::text[]) as orgs
         FROM users u
         LEFT JOIN org_members m ON m.user_id = u.id
         LEFT JOIN orgs o ON o.id = m.org_id
         GROUP BY u.id ORDER BY u.created_at DESC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type userSummary struct {
		ID              string    `json:"id"`
		Email           string    `json:"email"`
		Name            string    `json:"name"`
		IsPlatformAdmin bool      `json:"is_platform_admin"`
		CreatedAt       time.Time `json:"created_at"`
		Orgs            []string  `json:"orgs"`
	}
	var users []userSummary
	for rows.Next() {
		var u userSummary
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsPlatformAdmin, &u.CreatedAt, &u.Orgs); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if u.Orgs == nil {
			u.Orgs = []string{}
		}
		users = append(users, u)
	}
	if users == nil {
		users = []userSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": users})
}
