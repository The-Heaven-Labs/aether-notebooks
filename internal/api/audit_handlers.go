package api

import (
	"net/http"
	"strconv"

	"github.com/heavenlabs/hnb/internal/audit"
)

type auditListResponse struct {
	Entries []audit.Entry `json:"entries"`
	Total   int           `json:"total"`
}

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	q := r.URL.Query()

	limit := 100
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	params := audit.QueryParams{
		OrgID:        claims.OrgID,
		Limit:        limit,
		Offset:       offset,
		Action:       q.Get("action"),
		UserID:       q.Get("user_id"),
		ResourceType: q.Get("resource_type"),
		ResourceID:   q.Get("resource_id"),
	}

	entries, err := s.audit.Query(ctx, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if entries == nil {
		entries = []audit.Entry{}
	}

	total, err := s.audit.Count(ctx, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "count failed")
		return
	}

	writeJSON(w, http.StatusOK, auditListResponse{
		Entries: entries,
		Total:   total,
	})
}
