package api

import (
	"net/http"
	"strconv"

	"github.com/the-heaven-labs/aether/internal/audit"
)

type auditListResponse struct {
	Entries []audit.Entry `json:"entries"`
	Total   int           `json:"total"`
}

// @Summary List audit logs
// @Description Returns audit log entries for the organization
// @Tags audit
// @Produce json
// @Param limit query int false "Maximum number of entries (max 500)"
// @Param offset query int false "Number of entries to skip"
// @Param action query string false "Filter by action"
// @Param user_id query string false "Filter by user ID"
// @Param user query string false "Filter by user email"
// @Param resource_type query string false "Filter by resource type"
// @Param resource_id query string false "Filter by resource ID"
// @Param from query string false "Filter by start date"
// @Param to query string false "Filter by end date"
// @Success 200 {object} auditListResponse
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/audit [get]
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
		UserEmail:    q.Get("user"),
		ResourceType: q.Get("resource_type"),
		ResourceID:   q.Get("resource_id"),
		DateFrom:     q.Get("from"),
		DateTo:       q.Get("to"),
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
